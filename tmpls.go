/*
Package tmpls provides a templates and data manager for
[pkg/github.com/valyala/fasttemplate].

Because [pkg/github.com/valyala/fasttemplate] is minimalisitic, the need
for this wrapper arose. Two template directives were implemented – `wrapper`
and `include`. These make this simple templates manager powerful enough for
big and complex sites or generating any text output.

The main template can be compiled from several files – as many as you need –
with the simple approach of wrapping and including files recursively.
fasttemplate's TagFunc allows us to keep logic into our Go code and prepare
pieces of the output as needed. See the tests and sample templates for usage
examples.
*/
package tmpls

import (
	"bytes"
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

	ft "github.com/valyala/fasttemplate"
)

// TagFunc is an alias for fasttemplate.TagFunc
type TagFunc = ft.TagFunc

var spf = fmt.Sprintf

// path => slurped file content
type filesMap map[string]string

// DataMap is a stash for replacement into templates. Can have the same value types, needed
// for fasttemplate:
//   - []byte - the fastest value type
//   - string - convenient value type
//   - TagFunc - flexible value type
type DataMap map[string]any

// Tmpls manages files and data for fasttemplate.
type Tmpls struct {
	// A map for replacement into templates
	DataMap DataMap
	// file name => file contents
	files filesMap
	// compiled templates
	compiled filesMap
	// File extension of the templates, for example: ".htm".
	Ext string
	// Root folder, where template files reside, fo example "./templates"
	root string
	// Pair of Tags, for example:  "${", "}".
	Tags [2]string
	// How deeply files can be included into each other.
	// Default: 3 starting from 0 in the main template.
	IncludeLimit int
	// To wait for storeCompiled() to finish.
	wg sync.WaitGroup
	// Any logger defining Debug, Error, Info, Warn
	Logger Logger
}

const defaultLogHeader = `${prefix}:${time_rfc3339}:${level}:${short_file}:${line}`

// New instantiates a new [Tmpls] struct and returns it. Prepares [DataMap] and
// loads all template files from disk under the given `root` if `loadFiles` is
// true. Otherwise postpones the loading of the needed file until
// [Tmpls.Compile] is invoked automatically in [Tmpls.Execute].
func New(root string, ext string, tags [2]string, loadFiles bool) (*Tmpls, error) {
	t := &Tmpls{
		DataMap:      make(DataMap, 5),
		compiled:     make(filesMap, 5),
		files:        make(filesMap, 5),
		Ext:          ext,
		Tags:         tags,
		IncludeLimit: 3,
		Logger:       log.New("tmpls"),
	}
	if err := t.findRoot(root); err != nil {
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
	return t, nil
}

// Compile composes a template and returns its content or an error. This means:
//   - The file is loaded from disk using [Tmpls.LoadFile] for use by
//     [Tmpls.Execute].
//   - if the template contains `${wrapper some/file}`, the wrapper file is
//     wrapped around it.
//   - if the template contains any `${include some/file}` the files are
//     loaded, wrapped (if there is a wrapper directive in them) and included
//     at these places without rendering any placeholders. The inclusion
//     is done recursively. See *Tmpls.IncludeLimit.
//   - The compiled template is stored in a private map[filename(string)]string,
//     attached to *Tmpls for subsequent use during the same run of the
//     application. The content of the compiled template is stored on disk with
//     a sufix "c", attached to the extension of the file in the same directory
//     where the template file resides. The storing of the compiled file is
//     done concurently in a goroutine while being executed.
//   - On the next run of the application the compiled file is simply loaded
//     and its content retuned. All the steps above are skipped.
//
// Panics in case the *Tmpls.IncludeLimit is reached. If you have deeply nested
// included files you may need to set a bigger integer. This method is suitable
// for use in a ft.TagFunc to compile parts to be replaced in bigger templates.
func (t *Tmpls) Compile(path string) (string, error) {
	path = t.toFullPath(path)
	if text, e := t.loadCompiled(path); e == nil {
		return text, nil
	}
	t.Logger.Debugf("Compile('%s')", path)
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
	t.compiled[path] = text
	t.wg.Add(1)
	go t.storeCompiled(path, t.compiled[path])
	return t.compiled[path], nil
}

func (t *Tmpls) loadCompiled(fullPath string) (string, error) {
	if text, ok := t.compiled[fullPath]; ok {
		return text, nil
	}
	t.Logger.Debugf("loadCompiled('%s')", fullPath)
	fullPath = fullPath + "c"
	if fileIsReadable(fullPath) {
		if data, err := os.ReadFile(fullPath); err != nil {
			return "", err
		} else {
			t.compiled[fullPath] = string(data)
			return t.compiled[fullPath], nil
		}
	}
	return "", errors.New(spf("File '%s' could not be read!", fullPath))
}

func (t *Tmpls) storeCompiled(fullPath, text string) {
	defer t.wg.Done()
	t.Logger.Debugf("storeCompiled('%s')", fullPath)
	err := os.WriteFile(fullPath+"c", []byte(text), 0600)
	if err != nil {
		t.Logger.Panic(err)
	}
}

var ftExec = ft.Execute

// Execute compiles (if needed) and executes the passed template using
// fasttemplate.Execute. The path is resolved by prefixing the root folder
// and attaching the extension, passed to [New], if the passed file is only a
// base name. Example: `path := "view"` => `/home/user/app/templates/view.htm`.
func (t *Tmpls) Execute(w io.Writer, path string) (int64, error) {
	text, err := t.Compile(path)
	if err != nil {
		return 0, err
	}
	length, err := ftExec(text, t.Tags[0], t.Tags[1], w, t.DataMap)
	t.wg.Wait()
	return length, err

}

// FtExecStd is a wrapper for fasttemplate.ExecuteStd(). Useful for preparing
// partial templates which will be later included in the main template, because
// it keeps unknown placeholders untouched.
func (t *Tmpls) FtExecStd(tmpl string, w io.Writer, data map[string]any) (int64, error) {
	return ft.ExecuteStd(tmpl, t.Tags[0], t.Tags[1], w, data)
}

func (t *Tmpls) loadFiles() error {
	return filepath.WalkDir(t.root, func(path string, d fs.DirEntry, err error) error {
		if strings.HasSuffix(path, t.Ext) {
			if _, err = t.LoadFile(path); err != nil {
				return err
			}
		}
		return err
	})
}

// LoadFile is used to load a template from disk or from cache, if already
// loaded before.  Returns the template text or error if template cannot be
// loaded.
func (t *Tmpls) LoadFile(path string) (string, error) {
	path = t.toFullPath(path)
	if text, ok := t.files[path]; ok && len(text) > 0 {
		return text, nil
	}
	if fileIsReadable(path) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		t.files[path] = string(data)
		return t.files[path], nil
	}
	return "", errors.New(spf("File '%s' could not be read!", path))
}

func (t *Tmpls) toFullPath(path string) string {
	if !strings.HasSuffix(path, t.Ext) {
		path = path + t.Ext
	}
	if !strings.HasPrefix(path, t.root) {
		path = filepath.Join(t.root, path)
	}
	return path
}

// MergeDataMap adds entries into the data map, used by
// fasttemplate.Execute(...) in [Tmpls.Execute]. If entries with the same key
// exist, they will be overriden with the new values.
func (t *Tmpls) MergeDataMap(data DataMap) {
	for k, v := range data {
		t.DataMap[k] = v
	}
}

// Tries to return an existing absolute path to the given root path. If the
// provided root is relative, the function expects the root to be relative to
// the Executable file or to the current working directory. If the root does
// not exist, this function panics.
func (t *Tmpls) findRoot(root string) error {
	if !filepath.IsAbs(root) {
		byExe := filepath.Join(findBinDir(), root)
		if dirExists(byExe) {
			t.root = byExe
			return nil
		}
		// Now try by CWD
		byCwd, _ := filepath.Abs(root)
		if dirExists(byCwd) {
			t.root = byCwd
			return nil
		} else { // this is dead code but Go compiler made me write it
			return fmt.Errorf("Tmplsroot directory '%s' does not exist!", byCwd)
		}
	}

	if dirExists(root) {
		t.root = root
		return nil
	} else { // this is dead code but Go compiler made me write it
		return fmt.Errorf("Templates root directory '%s' does not exist!", root)
	}
}

func dirExists(path string) bool {
	finfo, err := os.Stat(path)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return false
	}
	if finfo.IsDir() {
		return true
	}
	return false
}

func fileIsReadable(path string) bool {
	finfo, err := os.Stat(path)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return false
	}
	if finfo.Mode().IsRegular() && finfo.Mode().Perm()&0400 == 0400 {
		return true
	}
	return false
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
func (t *Tmpls) include(text string) (string, error) {
	restr := spf(`(?m)\Q%s\E(include\s+([/\.\w]+))\Q%s\E`, t.Tags[0], t.Tags[1])
	reInclude := regexp.MustCompile(restr)
	matches := reInclude.FindAllStringSubmatch(text, -1)
	t.Logger.Debugf("include: %s", matches)
	included := bytes.NewBuffer([]byte(""))
	howMany := len(matches)
	if howMany > 0 {
		data := make(map[string]any, howMany)
		for _, m := range matches {
			if t.detectInludeRecurionLimit() {
				panic(spf("Limit of %d nested inclusions reached"+
					" while trying to include %s", t.IncludeLimit, m[2]))
				//return text, nil
			}
			includedFileContent, err := t.LoadFile(m[2])
			if err != nil {
				t.Logger.Warnf("err:%s", err.Error())
				return text, err
			}
			includedFileContent, err = t.wrap(strings.Trim(includedFileContent, "\n"))
			if err != nil {
				return text, err
			}
			data[m[1]], err = t.include(includedFileContent)
			if err != nil {
				return text, err
			}
		}

		// Keep unknown placeholders for the main Execute!
		if _, err := t.FtExecStd(text, included, data); err != nil {
			return text, err
		}
		return included.String(), nil
	}
	return text, nil
}

// If a template file contains `${wrap some/file}`, then `some/file` is
// loaded and the content is put in it in place of `${content}`. This
// means that `content` tag is special in wrapper templates and cannot be used
// as a regular placeholder. Only one `wrapper` directive is allowed per file.
// Returns the wrapped template text or the passed text with error.
func (t *Tmpls) wrap(text string) (string, error) {
	re := spf(`(?m)\n?\Q%s\E(wrapper\s+([/\.\w]+))\Q%s\E\n?`, t.Tags[0], t.Tags[1])
	reWrapper := regexp.MustCompile(re)
	// allow only one wrapper
	match := reWrapper.FindAllStringSubmatch(text, 1)
	t.Logger.Debugf("wrapper: %s", match)
	if len(match) > 0 && len(match[0]) == 3 {
		wrapper, err := t.LoadFile(string(match[0][2]))
		if err != nil {
			return text, err
		}
		text = reWrapper.ReplaceAllString(strings.Trim(text, "\n"), "")
		text = strings.Replace(wrapper, spf("%scontent%s", t.Tags[0], t.Tags[1]), text, 1)
	}
	return text, nil
}

// frames = 1 : direct recursion - calls it self - fine.
// frames < t.IncludeLimit : direct recursion - calls it self - still fine.
// frames == t.IncludeLimit : indirect - some caller on t.IncludeLimit call
// frame still calls the same function - too many recursion levels - stop.
func (t *Tmpls) detectInludeRecurionLimit() bool {
	pcme, _, _, _ := runtime.Caller(1)
	detailsme := runtime.FuncForPC(pcme)
	pc, _, _, _ := runtime.Caller(1 + t.IncludeLimit)
	details := runtime.FuncForPC(pc)
	return (details != nil) && detailsme.Name() == details.Name()
}

// Logger is implemented by gommon/log
type Logger interface {
	Debug(args ...any)
	Debugf(format string, args ...any)
	DisableColor()
	Error(args ...any)
	Errorf(format string, args ...any)
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
