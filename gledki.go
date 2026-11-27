/*
Package gledki provides a templates and data manager, and a preprocessor for
[fasttemplate].

Because fasttemplate is minimalistic, the need for this wrapper arose. Two
template directives were implemented – `wrapper` and `include`. They make
[gledki] powerful enough for use in big and complex web applications.
Comments `${# …}` are removed when a template is compiled; a line with only
comments is removed entirely.

The main template (the one which partial path you pass as argument to
[Gledki.Exec]) can be compiled from several files – as many as you need –
with the simple approach of wrapping and including partial files recursively.
[TagFunc] allows us to keep logic into our Go code and prepare pieces of the
output as needed. Leveraging cleverly TagFunc gives us complete separation of
concerns – output and logic. In a TagFunc we can invoke [Gledki.Compile] to
prepare partial templates, make any calculations and prepare the output for
replacement in the current template. No need to learn a new template language.
The possibilities of this simple but powerful technique inspired me to write
this wrapper.
Ah, and „gledki(гледки)“ means "views" in Bulgarian.

See the tests and sample templates for usage examples.

# Terms

The documentation uses these words for the different forms of a template:

  - template file: a file on disk under one of the [Gledki.Roots], for
    example `view.htm`. `path` is the path, passed to a method, for example
    `view`; `fullPath` is the full path to the template file, found by
    walking the Roots.
  - template text: the content of a template file, as it is read from disk.
  - compiled text: the text, produced from a template text by the directives
    `wrapper` and `include`, with the comments `${# …}` removed and the
    spaces around the content of every tag trimmed. The directives are
    already replaced in it; only the placeholders for the values in the
    [Stash] are left. [Gledki.Compile]
    returns it.
  - compiled file: the file `fullPath` + [CompiledSuffix] (for example
    `view.htmc`) next to the template file. It contains the compiled text.
  - parsed template: a [fasttemplate.Template] object, made from a compiled
    text. In it every placeholder is already found, so executing it only
    writes the output.
  - compiled template: the compiled text together with its parsed template
    and the ready calls in it (see Calls), kept in memory.

# Calls

Since v0.7.11 a template can call a few Go functions (builtins) with values
from the [Stash], so that fewer values have to be assigned as stash values in
Go:

	<img src="${IfElse(weHaveAUser, `/img/user.svg`, `/img/users-plus.svg`)}">
	${If(showForm, Exec(`partials/form`))}

A template sees only the stash: a name in it is a stash key, never a variable,
function or package of the Go program. It can call only the builtins below; a
call of any other function stays a placeholder. So a template can use the
values, which the program puts in the stash, but it cannot reach anything else
in the program, except the functions, which the program registers for templates
(see Functions of the user).

The builtins are [If], [IfElse], [Unless] (the opposite of If, since a
condition cannot be negated in a template), Exec (it executes another template
file with the same stash, like [Gledki.Exec]) and Set (${Set(`key`, value)}
writes the value under key in the stash and writes nothing). The condition of
If, IfElse and Unless must be a bool in the stash; anything else (also a
missing key) panics, and the panic names the template (with the chain of Exec
calls), the call and the argument. An argument is one of:

  - a stash key (`IsUserSignedIn`): its value from the stash;
  - a string literal in backticks or double quotes (`partials/form`);
  - a nested call (`Exec(`partials/form`)`): it does not run before the
    outer call. The outer function returns a [TagFunc] (a
    closure), which runs the nested call when it is called. In the example
    above If gets such a TagFunc from Exec(`partials/form`) and calls it only
    when showForm is true, so only then is the form executed.

Nothing else is allowed: no operators, no numbers, no dot notation yet. The
logic stays in Go; the template only calls. The tag content is parsed with
[go/parser.ParseExpr] once, when the template is compiled; a wrong call of a
builtin is a compile error with the template file and the line, and a call of
an unknown function stays a placeholder and is logged as a warning. When the
template is executed, the stash wins: if it has a key equal to the tag content,
its value is written, and the call does not run.

Only [Gledki.Exec] (and [Gledki.Execute]) run calls; [Gledki.FtExec] and its
siblings execute the compiled text with fasttemplate and know nothing about
them. An end tag inside a literal (`${IfElse(a, `}`, `x`)}`) ends the tag, so
such a call is a compile error. Nested Exec calls are limited by
[Gledki.IncludeLimit]; a [TagFunc] in the stash, which calls [Gledki.Exec]
itself, starts the count again.

# How a call runs

When the execution reaches a call tag, three things happen. First the arguments
get their values: a stash key its value, a literal its text and a nested call a
TagFunc, which runs that call. Then the function is called with these values;
it returns a value to write (nil, string, []byte or TagFunc). If and IfElse,
for example, return a TagFunc, which writes the chosen argument. Last, the
result is written at the place of the tag, as a stash value is: a TagFunc is
called with the writer. So If and IfElse are higher-order functions: they take
functions and return a function, and nothing is executed until a TagFunc is
called with the writer. A function of the user gets a nested call as a TagFunc
too, so its parameter for it must be any or TagFunc.

# Functions of the user

The program can register its own functions for the templates with [Func], made
ready by one of the adapters [StashFunc0], [StashFunc1] and [StashFunc2] (the
number is the count of arguments in the template).  Register them in init(),
before any template is compiled:

	func init() {
		gledki.Func("Selected", gledki.StashFunc2(selected))
	}

	// selected returns "selected", when the value of key in the stash is want.
	func selected(s gledki.Stash, key, want string) string {
		if fmt.Sprint(s[key]) == want {
			return "selected"
		}
		return ""
	}

and call them like the builtins:

	<option ${Selected(`currency`, `BGN`)}>BGN</option>.

The stash of the current execution is passed first, and the function may also
write to it: a written value is seen by the tags after the call and by the
templates executed with Exec after it. A written key, equal to the content of a
later call tag, wins over that call, as any stash key does.
*/
package gledki

import (
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
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

// Stash is an alias for `map[string]any` for replacement into templates. It
// has the same value types' requirements as for fasttemplate:
//   - []byte - the fastest value type;
//   - string - convenient value type;
//   - TagFunc - flexible value type.
type Stash = map[string]any

// Gledki manages files and data for fasttemplate.
type Gledki struct {
	// A map for replacement into templates by [Gledki.Execute].
	//
	// Deprecated: The Stash is shared by all goroutines, which use this
	// Gledki, so it cannot be used by many of them at the same time. Pass a
	// Stash to [Gledki.Exec] instead. Removed in v0.8.0.
	Stash Stash
	// file name => file contents
	files filesMap
	// compiled and parsed templates
	compiled compiledMap
	// File extension of the templates, for example: ".htm".
	Ext string
	// Root folders, where template files reside, for example
	// ["./templates","example.com","themeX"]. They will be walked up in the
	// order they are provided to find the template file, passed to
	// [Gledki.Execute]. The first found template is used.
	Roots []string
	// Pair of Tags, for example:  "${", "}".
	Tags [2]string
	// How deeply files can be wrapped and included into each other. The main
	// template file is at depth 0; every `include` and every `wrapper` puts
	// the file it names one level deeper. Default: 4 (one wrapper and three
	// levels of inclusions in it). It also limits how deeply the builtin Exec
	// can be called from templates, executed by other Exec calls (see Calls in
	// the package documentation).
	IncludeLimit int
	// Any logger defining Debug, Error, Info, Warn... See tmpls.Logger.
	Logger Logger
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
// that every change in a template file is seen without restarting the
// application. Then [Gledki.Compile] compiles the template file every time: it
// does not take the compiled template from memory or the compiled text from
// the compiled file (see [CompiledSuffix]) and does not store them there;
// [Gledki.LoadFile] reads the template file every time; and the full paths of the template files are not
// remembered. Set it once, before the first template is compiled, and do not
// change it while templates are compiled in other goroutines.
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
		IncludeLimit: 4,
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
Compile makes the compiled text of the template file `path` and returns it
or an error (see Terms in the package documentation). The compiled text can
be used by any of the methods [Gledki.FtExec], [Gledki.FtExecStd],
[Gledki.FtExecString] or [Gledki.FtExecStringStd]. This means:
  - The template text is read from the template file using [Gledki.LoadFile].
  - If the template text contains `${wrapper some/file}`, the template text of
    the wrapper file is wrapped around it: the template text is put in place of
    `${content}` in the wrapper file. A `${content}` in a file included into
    the wrapper file stays a placeholder for a value in the [Stash]. Only the
    first `wrapper` directive in a template file is used; every other `wrapper`
    directive in the same file is removed like a comment, its file is not read,
    and a warning with the template file and the line is logged. A wrapper file
    can have its own wrapper.
  - If the template text contains any `${include some/file}`, the template
    texts of these files are read, wrapped (if there is a wrapper directive
    in them) and included at these places without replacing any
    placeholders. The inclusion is done recursively. See
    [Gledki.IncludeLimit]. The result is the compiled text.
  - A wrapper or included file, which cannot be read (for example
    `${include no/such/file}`), is an error, which names the template file
    and the line of the directive. A template text with a NUL byte is an
    error too.
  - Every call of a builtin (see Calls in the package documentation) is
    checked: a wrong call is an error with the template file and the line; a
    call of an unknown function stays a placeholder and is logged as a
    warning, like a start tag without an end tag.
  - The compiled text is parsed into a parsed template (a
    [fasttemplate.Template] object): every placeholder is found once, so that
    executing it later only writes the output.
  - Every template text (of the template file, of its wrapper and of the
    included files) is checked for a start tag without an end tag, also one
    followed by another start tag before the next end tag. The first one is
    logged as a warning with the template file and the line, and
    [Gledki.MustCompile] panics with it. As in v0.7.8 it is not an error:
    when the compiled text cannot be parsed, it is executed without parsing
    it in advance and such a start tag is written as it is.
  - If [CacheTemplates] is true, the compiled template (the compiled text
    and its parsed template) is kept in memory, in a private map attached to
    *Gledki (the current instance), for subsequent use during the same run of
    the application. The compiled text is also written to the compiled file
    (the template file with [CompiledSuffix] attached to its extension, in
    the same directory). Compile returns after the compiled file is written.
  - If CacheTemplates is true, the next call (in the same run, from memory)
    or the next run of the application (from the compiled file) simply takes
    the compiled template. All the steps above are skipped, except that the
    compiled text from a compiled file is parsed once. A compiled file, whose
    compiled text cannot be parsed, is not used: the compiled text is made
    again from the template files (so that the template file with the start
    tag without an end tag is reported) and the compiled file is overwritten.
    Then the compiled template is kept in memory, so the compiled file is not
    read again in the same run.
  - If CacheTemplates is false (during development), nothing is kept or
    taken: every call makes the compiled text again from the template files
    and parses it, so every change in them is seen at once.

Panics in case the *Gledki.IncludeLimit is reached, also by a cycle of
wrappers or inclusions. If you have deeply nested wrapped or included files
you may need to set a bigger integer. This method is suitable
for use in a ft.TagFunc to prepare parts of the output to be replaced in the
main template.
*/
func (t *Gledki) Compile(path string) (string, error) {
	compiledT, err := t.compile(path)
	if err != nil {
		return "", err
	}
	return compiledT.text, nil
}

/*
MustCompile does the same as [Gledki.Compile], but panics in case of any
error: a template file, which cannot be read, or a start tag without an end
tag in one of the template texts. The panic message names the template file
and the line of the start tag. Use it during development (with
[CacheTemplates] false, so that every template file is checked) or at the
start of the application to see at once that a template is broken.
*/
func (t *Gledki) MustCompile(path string) string {
	compiledT, err := t.compile(path)
	if err == nil {
		err = compiledT.tagErr
	}
	if err != nil {
		t.Logger.Panic(err)
	}
	return compiledT.text
}

// compile does the work of [Gledki.Compile] and returns the compiled template
// (the compiled text and its parsed template) of the template file `path`.
// [Gledki.Compile] returns only the compiled text; [Gledki.Execute] executes
// the parsed template.
func (t *Gledki) compile(path string) (compiledT *compiledTemplate, err error) {
	path = t.toFullPath(path)
	if CacheTemplates {
		if compiledT, err = t.loadCompiled(path); err == nil {
			return compiledT, nil
		}
	}
	// t.Logger.Debugf("compile('%s')", path)

	var text string
	if text, err = t.LoadFile(path); err != nil {
		return nil, err
	}
	tCheck := &tagCheck{}
	if text, err = t.preprocess(path, text, 0, tCheck); err != nil {
		return nil, err
	}
	compiledT = t.parse(text)
	compiledT.tagErr = tCheck.err
	if compiledT.tpl == nil && compiledT.tagErr == nil {
		compiledT.tagErr = fmt.Errorf("template file %s: start tag %q without end tag", path, t.Tags[0])
	}
	if compiledT.tagErr != nil {
		t.Logger.Warn(compiledT.tagErr)
	}
	if CacheTemplates {
		t.compiled.store(path, compiledT)
		t.storeCompiled(path, text)
	}
	return compiledT, nil
}

// loadCompiled returns the compiled template of the template file `fullPath`
// with a nil error. It takes it from memory, if it is there. Otherwise it
// reads the compiled text from the compiled file (fullPath +
// [CompiledSuffix]), parses it into a parsed template and keeps both in
// memory for the next call. If the compiled file cannot be read or its
// compiled text cannot be parsed, it returns nil and the error.
// TODO: Decide whether the compiled file has to contain also a private stash
// of the template (for example as a gob), which is merged at execution time
// with the Stash used to execute and render the template.
func (t *Gledki) loadCompiled(fullPath string) (*compiledTemplate, error) {
	if compiledT, ok := t.compiled.load(fullPath); ok {
		return compiledT, nil
	}
	// t.Logger.Debugf("loadCompiled('%s')", fullPath)
	data, err := os.ReadFile(fullPath + CompiledSuffix)
	if err != nil {
		return nil, fmt.Errorf("compiled file: %v", err)
	}
	// TODO: Changes for how compiled files are kept in memory, depending on
	// the changed file format, go here.
	compiledT := t.parse(string(data))
	if compiledT.tpl == nil {
		// Compile it again from the template files, so that the template
		// file with the start tag without an end tag is reported.
		return nil, fmt.Errorf("compiled file %s: start tag %q without end tag",
			fullPath+CompiledSuffix, t.Tags[0])
	}
	if compiledT.callErr != nil {
		// Compile it again from the template files, so that the wrong call
		// is reported with the template file and the line.
		return nil, fmt.Errorf("compiled file %s: %w", fullPath+CompiledSuffix, compiledT.callErr)
	}
	t.compiled.store(fullPath, compiledT)
	return compiledT, nil
}

// storeCompiled writes the compiled text `text` to the compiled file of the
// template file `fullPath` (fullPath + [CompiledSuffix]). It panics if the
// compiled file cannot be written.
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
// TODO: Decide whether the file has to contain also a private stash of the
// template (for example as a gob), with ways to resolve dot notations and
// `execute` directives at execution time.
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

// Execute takes the compiled template of the template file `path` (making it
// first, if needed – see [Gledki.Compile]) and executes its parsed template
// with the values from [Gledki.Stash] and clears Gledki.Stash afterwards.
//
// The path is resolved by prefixing the root folder and attaching the
// extension, passed to [New], if the passed path is only a base name. Example:
// `path := "view"` => `/home/user/app/templates/view.htm`.
//
// Deprecated: Execute uses the shared [Gledki.Stash], so it cannot be used by
// many goroutines at the same time. In v0.8.0 Execute takes the stash as an
// argument, like [Gledki.Exec]. Until then use Exec.
func (t *Gledki) Execute(w io.Writer, path string) (int64, error) {
	defer clear(t.Stash)
	return t.Exec(w, path, t.Stash)
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

// loadFiles walks the paths in [Gledki.Roots] and loads each template file
// (a file with extension [Gledki.Ext]).
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

// MergeStash adds entries into [Gledki.Stash], used by [Gledki.Execute]. If
// entries with the same key exist, they will be overridden with the new
// values.
//
// Deprecated: Gledki.Stash is shared by all goroutines, which use this
// Gledki. Pass a Stash to [Gledki.Exec] instead. Removed in v0.8.0.
func (t *Gledki) MergeStash(data Stash) {
	maps.Copy(t.Stash, data)
}

// findRoots tries to find the given root paths and stores them as absolute. If
// the provided roots are relative, the function expects the roots to be
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
