/*
Package gledki provides a templates and data manager for
[fasttemplate].

Because fasttemplate is minimalisitic, the need for this wrapper arose. Two
template directives were implemented – `wrapper` and `include`. They make
[gledki] powerful enough for use in big and complex web applications.

The main template (the one which partial path you pass as argument to
[Gledki.Execute]) can be compiled from several files – as many as you need –
with the simple approach of wrapping and including partial files recursively.
[TagFunc] allows us to keep logic into our Go code and prepare pieces of the
output as needed. Leveraging cleverly TagFunc gives us complete separation of
concerns. In TagFunc we can invoke [Gledki.Compile] to preprare partial
templates, make any calculatuons and prepare the output for replacement in the
main template. No need to learn a new template language. The possibilities of
this simple but powerful technique ispired me to write this wrapper.
Ah, and „gledki(гледки)“ means "views" in Bulgarian.

See the tests and sample templates for usage examples.
*/
package gledki

import (
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/labstack/gommon/log"
	"github.com/valyala/fasttemplate"
)

/*
TagFunc is an alias for [fasttemplate.TagFunc].
TagFunc can be used as a substitution value in the map passed to Execute*.
Execute* functions pass tag (placeholder) name in 'tag' argument.

TagFunc must be safe to call from concurrently running goroutines.

TagFunc must write contents to w and return the number of bytes written.
*/
type TagFunc = fasttemplate.TagFunc

// filesMap keeps the content of files by their full path (full path => file
// content). One [Gledki] instance is often used by many goroutines at the same
// time (for example one per HTTP request), and the first requests may load or
// compile the same template at the same moment. A plain map would be changed
// and read by them at the same time (a data race). So filesMap keeps the
// entries in a [sync.Map], which is safe for this. Every entry is written once
// and then only read – the case for which sync.Map is made. The zero value is
// ready for use. Do not copy a filesMap after first use.
type filesMap struct {
	m sync.Map
}

// load returns the content stored for `path` and true, or "" and false if
// nothing is stored for it.
func (f *filesMap) load(path string) (string, bool) {
	text, ok := f.m.Load(path)
	if !ok {
		return "", false
	}
	return text.(string), true
}

// store keeps `text` as the content for `path`.
func (f *filesMap) store(path, text string) {
	f.m.Store(path, text)
}

// delete forgets the content for `path`.
func (f *filesMap) delete(path string) {
	f.m.Delete(path)
}

// Stash is an alias for `map[string]any` for replacement into templates. It
// has the same value types' requirements as for fasttemplate:
//   - []byte - the fastest value type;
//   - string - convenient value type;
//   - TagFunc - flexible value type.
type Stash = map[string]any

// Gledki manages files and data for fasttemplate.
type Gledki struct {
	// A map for replacement into templates
	Stash Stash
	// file name => file contents
	files filesMap
	// compiled templates
	compiled filesMap
	// File extension of the templates, for example: ".htm".
	Ext string
	// Root folders, where template files reside, for example
	// ["./templates","example.com","themeX"]. They will be wallked up in the
	// order they are provided to find the template file, passed to
	// [Gledki.Execute]. The first found is used.
	Roots []string
	// Pair of Tags, for example:  "${", "}".
	Tags [2]string
	// How deeply files can be included into each other.
	// Default: 3; starting from 0 in the main template.
	IncludeLimit int
	// Any logger defining Debug, Error, Info, Warn... See tmpls.Logger.
	Logger Logger
	// regex objects instantiated in New() and ready for use.
	res map[string]*regexp.Regexp
	// fullPaths remembers, for each template path passed to toFullPath, the
	// full path of the file which toFullPath found on disk. The next time the
	// same template is needed, its full path is taken from here, without
	// checking the disk again. A [sync.Map] is used, because one Gledki
	// instance is often used by many goroutines at the same time (for example
	// one per HTTP request), and because every entry is written once and then
	// only read – the case for which sync.Map is made.
	fullPaths sync.Map
}

const defaultLogHeader = `${prefix}:${level}:${short_file}:${line}`

// CompiledSuffix is appended to the extension of compiled templates.
var CompiledSuffix = "c"

var spf = fmt.Sprintf

// CacheTemplates can be set to false to disable caching during development, so
// that every change in a template file is seen at once. Then [Gledki.Compile]
// compiles the template every time: it does not take the compiled template
// from memory or from a compiled file on disk (see [CompiledSuffix]) and does
// not store it there; [Gledki.LoadFile] reads the file every time; and the
// full paths of the template files are not remembered. Set it once, before
// the first template is compiled, and do not change it while templates are
// compiled in other goroutines.
var CacheTemplates = true

/*
New instantiates a new [Gledki] struct and returns a reference to it. Prepares
[Stash] and loads all template files from disk under the given `roots` if
`loadFiles` is true. Otherwise postpones the loading of the needed file until
[Gledki.Compile] is invoked automatically in [Gledki.Execute].
*/
func New(roots []string, ext string, tags [2]string, loadFiles bool) (*Gledki, error) {
	t := &Gledki{
		Stash:        make(Stash, 15),
		Ext:          ext,
		Tags:         tags,
		IncludeLimit: 3,
		Logger:       log.New("gledki"),
	}
	if err := t.findRoots(roots); err != nil {
		return nil, err
	}
	t.Logger.SetOutput(os.Stderr)
	t.Logger.SetLevel(log.WARN)
	t.Logger.SetHeader(defaultLogHeader)
	if loadFiles {
		if err := t.loadFiles(); err != nil {
			return nil, err
		}
	}
	t.makeRegexes()
	return t, nil
}

// Must is a convenient wrapper for [New], which returns only &Gledki or panics
// in case of any error.
func Must(roots []string, ext string, tags [2]string, loadFiles bool) *Gledki {
	gl, err := New(roots, ext, tags, loadFiles)
	if err != nil {
		panic(err.Error())
	}
	return gl
}

/*
Compile composes a template and returns its content or an error. This means:
  - The file is loaded from disk using [Gledki.LoadFile] for use by
    any of the methods [Gledki.Execute], [Gledki.FtExec], [Gledki.FtExecStd],
    [Gledki.FtExecString] or [Gledki.FtExecStringStd].
  - If the template contains `${wrapper some/file}`, the wrapper file is
    wrapped around it. Only one `wrapper` directive is allowed per file.
  - If the template contains any `${include some/file}` the files are
    loaded, wrapped (if there is a wrapper directive in them) and included
    at these places without rendering any placeholders. The inclusion
    is done recursively. See Gledki.IncludeLimit.
  - If [CacheTemplates] is true, the compiled template is kept in memory, in
    a private map attached to *Gledki (the current instance), for subsequent
    use during the same run of the application. It is also written to disk
    with a suffix (see [CompiledSuffix]), attached to the extension of the
    file, in the same directory where the template file resides. Compile
    returns after the file is written.
  - If CacheTemplates is true, the next call (in the same run, from memory)
    or the next run of the application (from the compiled file on disk)
    simply takes the compiled template. All the steps above are skipped.
  - If CacheTemplates is false (during development), nothing is kept or
    taken: every call compiles the template again from the files, so every
    change in them is seen at once.

Panics in case the *Gledki.IncludeLimit is reached. If you have deeply nested
included files you may need to set a bigger integer. This method is suitable
for use in a ft.TagFunc to preprare parts of the output to be replaced in the
main template.
*/
func (t *Gledki) Compile(path string) (text string, err error) {
	path = t.toFullPath(path)
	if CacheTemplates {
		if text, err = t.loadCompiled(path); err == nil {
			return text, err
		}
	}
	// t.Logger.Debugf("Compile('%s')", path)

	if text, err = t.LoadFile(path); err != nil {
		return text, err
	}
	if text, err = t.wrap(text); err != nil {
		return text, err
	}

	if text, err = t.include(text); err != nil {
		return text, err
	}
	if CacheTemplates {
		t.compiled.store(path, text)
		t.storeCompiled(path, text)
	}
	return text, err
}

func (t *Gledki) loadCompiled(fullPath string) (string, error) {
	if text, ok := t.compiled.load(fullPath); ok {
		return text, nil
	}
	// t.Logger.Debugf("loadCompiled('%s')", fullPath)
	data, err := os.ReadFile(fullPath + CompiledSuffix)
	if err != nil {
		return "", fmt.Errorf("compiled file: %v", err)
	}
	text := string(data)
	t.compiled.store(fullPath, text)
	return text, nil
}

// storeCompiled writes the compiled template `text` to the compiled file of
// the template `fullPath` (fullPath + [CompiledSuffix]). It panics if the file
// cannot be written.
//
// The file is replaced at once (atomically): `text` is first written to a new
// temporary file in the same directory, and then the temporary file is renamed
// to the compiled file. Writing straight into the compiled file would first
// empty it and then fill it. Another goroutine, which reads the compiled file
// in between (for example while serving another of the first requests after
// the start of the application), would get an empty template without an
// error and would render an empty page. A rename never shows a half written
// file: a reader gets either the old file or the new one. The name of the
// temporary file also ends with the extension of the compiled files, so
// cleaning them (for example `rm *.htmc`) removes a temporary file, which was
// left after a crash, too.
func (t *Gledki) storeCompiled(fullPath, text string) {
	// t.Logger.Debugf("storeCompiled('%s')", fullPath)
	compiledPath := fullPath + CompiledSuffix
	pattern := strings.TrimSuffix(filepath.Base(fullPath), t.Ext) + ".*" + t.Ext + CompiledSuffix
	tmp, err := os.CreateTemp(filepath.Dir(fullPath), pattern)
	if err != nil {
		t.Logger.Panic(err)
	}
	_, err = tmp.WriteString(text)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), compiledPath)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		t.Logger.Panic(err)
	}
}

var ftExec = fasttemplate.Execute

// Execute compiles (if needed) and executes the passed template using
// [fasttemplate.Execute]. The path is resolved by prefixing the root folder
// and attaching the extension, passed to [New], if the passed file is only a
// base name. Example: `path := "view"` => `/home/user/app/templates/view.htm`.
func (t *Gledki) Execute(w io.Writer, path string) (int64, error) {
	defer clear(t.Stash)

	text, err := t.Compile(path)
	if err != nil {
		return 0, err
	}
	length, err := ftExec(text, t.Tags[0], t.Tags[1], w, t.Stash)
	return length, err
}

// FtExec replaces the placeholders in `tmpl` with the values from `data` and
// writes the result directly to `w`. It is a wrapper of [fasttemplate.Execute],
// which uses the [Gledki.Tags] of this instance. Placeholders, which have no
// value in `data`, are replaced with nothing, like in [Gledki.FtExecString].
//
// Use it instead of FtExecString when the result goes to an [io.Writer]
// anyway (for example in a [TagFunc]): FtExecString first builds the whole
// result as a new string, which then has to be copied to the writer. FtExec
// does neither. Also, if a [TagFunc] in `data` returns an error, FtExec
// returns it, while FtExecString panics.
//
// Unlike [Gledki.Execute], FtExec does not compile a file and does not use or
// clear [Gledki.Stash]: it works only with the passed `tmpl` and `data`. So it
// is safe to use from many goroutines at the same time, each with its own
// `data`.
func (t *Gledki) FtExec(tmpl string, w io.Writer, data Stash) (int64, error) {
	return ftExec(tmpl, t.Tags[0], t.Tags[1], w, data)
}

// FtExecStd is a wrapper around [fasttemplate.ExecuteStd]. Useful for preparing
// partial templates which will be later included in the main template, because
// it keeps unknown placeholders untouched.
func (t *Gledki) FtExecStd(tmpl string, w io.Writer, data Stash) (int64, error) {
	return fasttemplate.ExecuteStd(tmpl, t.Tags[0], t.Tags[1], w, data)
}

// FtExecString is a wrapper of [fasttemplate.ExecuteString]. Useful for
// preparing partial templates which will be later included in the main
// template. It does not keep unknown tags.
func (t *Gledki) FtExecString(template string, data Stash) string {
	return fasttemplate.ExecuteString(template, t.Tags[0], t.Tags[1], data)
}

// FtExecStringStd is a wrapper for [fasttemplate.ExecuteStringStd]. Useful for
// preparing partial templates which will be later included in the main
// template, because it keeps unknown placeholders untouched. It can be used
// as a drop-in replacement for strings.Replacer
func (t *Gledki) FtExecStringStd(template string, data Stash) string {
	return fasttemplate.ExecuteStringStd(template, t.Tags[0], t.Tags[1], data)
}

func (t *Gledki) loadFiles() error {
	for _, root := range t.Roots {
		if err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
			if strings.HasSuffix(path, t.Ext) {
				if _, err = t.LoadFile(path); err != nil {
					return err
				}
			}
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// LoadFile is used to load a template from disk or, if [CacheTemplates] is
// true, from memory, if it was already loaded before. Returns the template
// text or error if template cannot be loaded.
func (t *Gledki) LoadFile(path string) (string, error) {
	path = t.toFullPath(path)
	if CacheTemplates {
		if text, ok := t.files.load(path); ok && text != `` {
			return text, nil
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	if CacheTemplates {
		t.files.store(path, text)
	}
	return text, nil
}

/*
MustLoadFile does the same as [Gledki.LoadFile], but panics in case the
template file cannot be loaded.
*/
func (t *Gledki) MustLoadFile(path string) string {
	partial, err := t.LoadFile(path)
	if err != nil {
		t.Logger.Panic(err)
	}
	return partial
}

// toFullPath returns the full path to the file of the template `path`. If
// `path` is without extension, it appends [Gledki.Ext]. Then it looks in the
// [Gledki.Roots] in their order and returns the first file, which exists and
// can be read. If no such file is found, it returns `path` (with the
// extension), and the caller gets an error when it tries to read the file.
//
// Looking for the file means opening it on disk, which is slow compared to the
// rest of the work. So when [CacheTemplates] is true, the found full path is
// remembered in [Gledki.fullPaths] and the next call with the same `path`
// returns it without touching the disk. A path, for which no file was found,
// is not remembered, so a file created later will still be found. When
// CacheTemplates is false (during development), nothing is remembered, so a
// file added to a root, which comes earlier in Roots, is used at once.
func (t *Gledki) toFullPath(path string) string {
	if fullPath, ok := t.fullPaths.Load(path); ok {
		return fullPath.(string)
	}
	requested := path
	if !strings.HasSuffix(path, t.Ext) {
		path += t.Ext
	}
	for _, root := range t.Roots {
		foundPath := path
		if !strings.HasPrefix(path, root) {
			foundPath = filepath.Join(root, path)
		}
		if isReadable(foundPath) {
			if CacheTemplates {
				t.fullPaths.Store(requested, foundPath)
			}
			return foundPath
		}
	}
	return path
}

// MergeStash adds entries into the [Stash], used by
// [fasttemplate.Execute] in [Gledki.Execute]. If entries with the same key
// exist, they will be overriden with the new values.
func (t *Gledki) MergeStash(data Stash) {
	maps.Copy(t.Stash, data)
}

// findRoots tries to find the given root paths and stores them as absolute. If
// the  provided roots are relative, the function expects the roots to be
// relative to the executable file or to the current working directory. If
// some of the roots does not exist, this function returns an error.
func (t *Gledki) findRoots(roots []string) error {
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			var absRoot string
			if absRoot = findDirUp(root); absRoot != "" {
				t.Roots = append(t.Roots, absRoot)
				continue
			}
			return fmt.Errorf(
				"gledki root directory '%s': %w! You have to create it",
				root, os.ErrNotExist)
		}

		if dirExists(root) {
			t.Roots = append(t.Roots, root)
			continue
		}
		return fmt.Errorf("gledki root directory '%s': %w", root, os.ErrNotExist)
	}
	return nil
}

func findDirUp(dir string) string {
	cwd, _ := os.Getwd()
	absRoot := filepath.Join(cwd, dir)
	if dirExists(absRoot) {
		return absRoot
	}
	for !dirExists(absRoot) {
		cwd = filepath.Dir(cwd)
		absRoot = filepath.Join(cwd, dir)
		if dirExists(absRoot) {
			return absRoot
		}
		if cwd[len(cwd)-1] == filepath.Separator {
			break
		}
	}
	return ""
}

func dirExists(path string) bool {
	finfo, err := os.Stat(path)
	return err == nil && finfo.IsDir()
}

func isReadable(path string) bool {
	fh, err := os.Open(path)
	_ = fh.Close()
	return err == nil
}

// Replaces all occurances of 'include path/to/template' in 'text' with the
// contents of the partial templates. Panics in case the t.IncludeLimit is
// reached. If you have deeply nested included files you may need to set a
// bigger integer.
func (t *Gledki) include(text string) (string, error) {
	re := t.res["include"]
	matches := re.FindAllStringSubmatch(text, -1)
	howMany := len(matches)
	if howMany > 0 {
		// t.Logger.Debugf("include: %#v", matches)
		stash := make(Stash, howMany)
		for _, m := range matches {
			if t.detectInludeRecursionLimit() {
				t.Logger.Panicf("Limit of %d nested inclusions reached"+
					" while trying to include %s", t.IncludeLimit, m[2])
				//return text, nil
			}
			includedFileContent, err := t.LoadFile(m[2])
			if err != nil {
				t.Logger.Warnf("err:%s", err.Error())
				return "", err
			}
			includedFileContent, err = t.wrap(strings.TrimSuffix(includedFileContent, "\n"))
			if err != nil {
				return "", err
			}
			stash[m[1]], err = t.include(includedFileContent)
			if err != nil {
				return "", err
			}
		}
		// Replace ${include file/name.ext} with file content, but keep
		// placeholders for the main Execute!
		return t.FtExecStringStd(text, stash), nil
	}
	return text, nil
}

// If a template file contains `${wrap some/file}`, then `some/file` is loaded
// and the content is put in it in place of `${content}`. This means that
// `content` placeholder is special in wrapper templates and cannot be used as
// a regular placeholder. Only one `wrapper` directive is allowed per file.
// Returns the wrapped template text or the passed text with error.
func (t *Gledki) wrap(text string) (string, error) {
	text = strings.TrimSuffix(text, "\n")
	re := t.res["wrap"]
	// allow only one wrapper
	match := re.FindStringSubmatch(text)
	if len(match) > 2 {
		// t.Logger.Debugf("wrapper: %#v", match)
		wrapperFile, err := t.LoadFile(string(match[2]))
		if err != nil {
			return "", err
		}
		wrapperFile = strings.TrimSuffix(wrapperFile, "\n")
		// remove the matched m[1] from text
		text = strings.Replace(text, match[1], "", 1)
		// replace content with text
		text = t.FtExecStringStd(wrapperFile, map[string]any{"content": text})
	}
	return text, nil
}

// frames = 1 : direct recursion - calls it self - fine.
// frames < t.IncludeLimit : direct recursion - calls it self - still fine.
// frames == t.IncludeLimit : indirect - some caller on t.IncludeLimit call
// frame still calls the same function - too many recursion levels - stop.
func (t *Gledki) detectInludeRecursionLimit() bool {
	pcme, _, _, _ := runtime.Caller(1)
	detailsme := runtime.FuncForPC(pcme)
	pc, _, _, _ := runtime.Caller(1 + t.IncludeLimit)
	details := runtime.FuncForPC(pc)
	return (details != nil) && detailsme.Name() == details.Name()
}

// Make a map[names]*regexp.Regexp for internal use by directives'
// implementations.
func (t *Gledki) makeRegexes() {
	t.res = make(map[string]*regexp.Regexp, 2)
	t.res = map[string]*regexp.Regexp{
		"wrap": regexp.MustCompile(spf(
			`(?m:(\Q%s\Ewrapper\s+([/\.\-\w]+)\Q%s\E[\r]?[\n]?))`, t.Tags[0], t.Tags[1])),
		"include": regexp.MustCompile(
			spf(`\Q%s\E(include\s+([/\.\-\w]+))\Q%s\E`, t.Tags[0], t.Tags[1])),
	}
}

// Logger is implemented by gommon/log on which we depend.
type Logger interface {
	Debug(args ...any)
	Debugf(format string, args ...any)
	DisableColor()
	Error(args ...any)
	Errorf(format string, args ...any)
	Fatal(i ...any)
	Fatalf(format string, args ...any)
	Info(args ...any)
	Infof(format string, args ...any)
	Panic(i ...any)
	Panicf(format string, args ...any)
	SetHeader(h string)
	SetLevel(v log.Lvl)
	SetOutput(w io.Writer)
	Warn(args ...any)
	Warnf(format string, args ...any)
}
