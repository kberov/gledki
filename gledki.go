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
his simple but powerful technique ispired me to write this wrapper.
Ah, and „gledki(гледки)“ means "views" in Bulgarian.

See the tests and sample templates for usage examples.
*/
package gledki

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/labstack/gommon/log"
	"github.com/valyala/fasttemplate"
)

// TagFunc is an alias for [fasttemplate.TagFunc].
type TagFunc = fasttemplate.TagFunc

// path => slurped file content
type filesMap map[string]string

// Stash is `map[string]any` for replacement into templates. It has
// the value types' requirements for fasttemplate:
//   - []byte - the fastest value type
//   - string - convenient value type
//   - TagFunc - flexible value type
type Stash map[string]any

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
	// Default: 3 starting from 0 in the main template.
	IncludeLimit int
	// To wait while the compiled template is being stored.
	wg sync.WaitGroup
	// Any logger defining Debug, Error, Info, Warn... See tmpls.Logger.
	Logger
	// regex objects instantiated in New() and ready for use.
	res map[string]*regexp.Regexp
}

const defaultLogHeader = `${prefix}:${time_rfc3339}:${level}:${short_file}:${line}`

// CompiledSuffix is appended to the extension of compiled templates.
var CompiledSuffix = "c"

var spf = fmt.Sprintf

// CacheTemplates can be set to false to disable caching of compiled templates
// both in memory and on disk during development.
var CacheTemplates bool = true

/*
New instantiates a new [Gledki] struct and returns a reference to it. Prepares
[Stash] and loads all template files from disk under the given `roots` if
`loadFiles` is true. Otherwise postpones the loading of the needed file until
[Gledki.Compile] is invoked automatically in [Gledki.Execute].
*/
func New(roots []string, ext string, tags [2]string, loadFiles bool) (*Gledki, error) {
	t := &Gledki{
		Stash:        make(Stash, 5),
		compiled:     make(filesMap, 5),
		files:        make(filesMap, 5),
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
    [Gledki.Execute].
  - if the template contains `${wrapper some/file}`, the wrapper file is
    wrapped around it. Only one `wrapper` directive is allowed per file.
  - if the template contains any `${include some/file}` the files are
    loaded, wrapped (if there is a wrapper directive in them) and included
    at these places without rendering any placeholders. The inclusion
    is done recursively. See Gledki.IncludeLimit.
  - The compiled template is stored in a private map[filename(string)]string,
    attached to *Gledki for subsequent use during the same run of the
    application. The content of the compiled template is stored on disk with a
    suffix (see [CompiledSuffix]), attached to the extension of the file in the
    same directory where the template file resides. The storing of the compiled
    file is done concurently in a goroutine while being executed.
  - On the next run of the application the compiled file is simply loaded
    and its content retuned. All the steps above are skipped.

Panics in case the *Gledki.IncludeLimit is reached. If you have deeply nested
included files you may need to set a bigger integer. This method is suitable
for use in a ft.TagFunc to preprare parts of the output to be replaced in the
main template.
*/
func (t *Gledki) Compile(path string) (string, error) {
	path = t.toFullPath(path)
	if text, e := t.loadCompiled(path); e == nil {
		return text, nil
	}
	// t.Logger.Debugf("Compile('%s')", path)
	text, err := t.LoadFile(path)
	if err != nil {
		return "", err
	}
	if text, err = t.wrap(text); err != nil {
		return text, err
	}

	if text, err = t.include(text); err != nil {
		return text, err
	}
	if CacheTemplates {
		t.compiled[path] = text
		t.wg.Add(1)
		go t.storeCompiled(path, t.compiled[path])
	}
	return text, nil
}

func (t *Gledki) loadCompiled(fullPath string) (string, error) {
	if text, ok := t.compiled[fullPath]; ok {
		return text, nil
	}
	// t.Logger.Debugf("loadCompiled('%s')", fullPath)
	data, err := os.ReadFile(fullPath + CompiledSuffix)
	if err != nil {
		return "", fmt.Errorf("compiled file: %v", err)
	}
	t.compiled[fullPath] = string(data)
	return t.compiled[fullPath], nil
}

func (t *Gledki) storeCompiled(fullPath, text string) {
	defer t.wg.Done()
	// t.Logger.Debugf("storeCompiled('%s')", fullPath)
	err := os.WriteFile(fullPath+CompiledSuffix, []byte(text), 0600)
	if err != nil {
		t.Logger.Panic(err)
	}
}

var ftExec = fasttemplate.Execute

// Execute compiles (if needed) and executes the passed template using
// [fasttemplate.Execute]. The path is resolved by prefixing the root folder
// and attaching the extension, passed to [New], if the passed file is only a
// base name. Example: `path := "view"` => `/home/user/app/templates/view.htm`.
func (t *Gledki) Execute(w io.Writer, path string) (int64, error) {
	text, err := t.Compile(path)
	if err != nil {
		return 0, err
	}
	length, err := ftExec(text, t.Tags[0], t.Tags[1], w, t.Stash)
	t.wg.Wait()
	return length, err
}

// FtExecStd is a wrapper around [fasttemplate.ExecuteStd]. Useful for preparing
// partial templates which will be later included in the main template, because
// it keeps unknown placeholders untouched.
func (t *Gledki) FtExecStd(tmpl string, w io.Writer, data Stash) (int64, error) {
	return fasttemplate.ExecuteStd(tmpl, t.Tags[0], t.Tags[1], w, data)
}

// FtExecString is a wrapper for [fasttemplate.ExecuteString]. Useful for
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
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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

// LoadFile is used to load a template from disk or from cache, if already
// loaded before. Returns the template text or error if template cannot be
// loaded.
func (t *Gledki) LoadFile(path string) (string, error) {
	path = t.toFullPath(path)
	if text, ok := t.files[path]; ok && len(text) > 0 {
		return text, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	t.files[path] = string(data)
	return t.files[path], nil
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

// If the template is without extension, appends it. Then finds the first
// matching file in the range of include paths and returns it.
func (t *Gledki) toFullPath(path string) string {
	if !strings.HasSuffix(path, t.Ext) {
		path = path + t.Ext
	}
	for _, root := range t.Roots {
		foundPath := path
		if !strings.HasPrefix(path, root) {
			foundPath = filepath.Join(root, path)
		}
		if isReadable(foundPath) {
			return foundPath
		} else {
			continue
		}
	}
	return path
}

// MergeStash adds entries into the [Stash], used by
// [fasttemplate.Execute] in [Gledki.Execute]. If entries with the same key
// exist, they will be overriden with the new values.
func (t *Gledki) MergeStash(data Stash) {
	for k, v := range data {
		t.Stash[k] = v
	}
}

// Tries to find existing absolute paths given the root paths. If the
// provided roots are relative, the function expects the roots to be relative to
// the Executable file or to the current working directory. If some of the
// roots does not exist, this function returns an error.
func (t *Gledki) findRoots(roots []string) error {
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			byExe := filepath.Join(findBinDir(), root)
			if dirExists(byExe) {
				t.Roots = append(t.Roots, byExe)
				continue
			}
			// Now try by CWD
			byCwd, _ := filepath.Abs(root)
			if dirExists(byCwd) {
				t.Roots = append(t.Roots, byCwd)
				continue
			} else {
				return fmt.Errorf("gledki root directory '%s' does not exist! You have to create it. ", byCwd)
			}
		}

		if dirExists(root) {
			t.Roots = append(t.Roots, root)
			continue
		} else {
			return fmt.Errorf("Gledki root directory %s: %w", root, os.ErrNotExist)
		}
	}
	return nil
}

func dirExists(path string) bool {
	finfo, err := os.Stat(path)
	if err != nil && errors.Is(err, os.ErrNotExist) || !finfo.IsDir() {
		return false
	}
	return true
}

func isReadable(path string) bool {
	fh, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = fh.Close()
	return true
}

func findBinDir() string {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return filepath.Dir(exe)
}

// Replaces all occurances of `include path/to/template` in `text` with the
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
	if len(match) > 0 {
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
