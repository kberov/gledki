package gledki

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/gommon/log"
)

var includePaths = []string{"./testdata/tpls", "./testdata/tpls/theme"}
var filesExt = ".htm"
var logger = log.New("gledki")
var tagsPair = [2]string{"${", "}"}
var out strings.Builder

// remove all compiled previously templates
func init() {
	sfx := filesExt + CompiledSuffix
	for _, dir := range includePaths {
		e := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
			if strings.HasSuffix(path, sfx) {
				_ = os.Remove(path)
			}
			return err
		})
		if e != nil {
			panic(e.Error())
		}
	}
	var lgbuf = bytes.NewBuffer([]byte(""))

	logger.SetOutput(lgbuf)
	logger.SetHeader(defaultLogHeader)
}

var v bool

func setVerboseLoggerOnce() {
	if logger == nil {

		panic("logger is nil")
	}
	if v {
		return
	}
	if v = testing.Verbose(); v {
		logger.SetLevel(log.DEBUG)
	}
}

func TestNew(t *testing.T) {
	setVerboseLoggerOnce()
	// load templates
	tpls, err := New(includePaths, filesExt, tagsPair, true)
	if err != nil {
		t.Error(err.Error())
		return
	}
	tpls.Logger = logger
	t.Logf("\ngledki.New loads all files in %s", includePaths)
	if filesCount(&tpls.files) == 0 {
		t.Error("templates must be loaded")
	}
	// do not load templates
	tpls, err = New(includePaths, filesExt, tagsPair, false)
	if err != nil {
		t.Error("Error New: ", err.Error())
	}
	if filesCount(&tpls.files) > 0 {
		t.Error("templates should not be loaded")
	}
	//Try to load nonreadable templates
	_ = os.Chmod(includePaths[0]+"/../tpls_bad/_noread.htm", 0300)
	_, err = New([]string{includePaths[0] + "/../tpls_bad"}, filesExt, tagsPair, true)
	if err != nil {
		t.Logf("Expected error from New: %s", err.Error())
		_ = os.Chmod(includePaths[0]+"/../tpls_bad/_noread.htm", 0400)
	} else {
		t.Error("Reading nonreadable file should have failed!")
	}
}

var data = Stash{
	"title":     "Здрасти",
	"body":      "<p>Едно тяло тук</p>",
	"lang":      "bg",
	"generator": "Образци",
	"included":  "вложена",
}

func TestExecute(t *testing.T) {
	setVerboseLoggerOnce()
	tpls, _ := New(includePaths, filesExt, tagsPair, false)
	tpls.Logger = logger
	tpls.Stash = data
	var out strings.Builder
	_, _ = tpls.Execute(&out, "view")
	outstr := out.String()
	t.Log(outstr)
	for k, v := range data {
		if !strings.Contains(outstr, v.(string)) {
			t.Errorf("output does not contain expected value for '%s': %s", k, v)
		}
	}

	//Change keys and check if they ar changed in the output
	// Same view with other data
	t.Log("=================")
	tpls.Stash = Stash{
		"title":     "Hello",
		"body":      "<p>A body here</p>",
		"lang":      "en",
		"generator": "Gledki",
		"included":  "included",
	}
	out.Reset()
	_, _ = tpls.Execute(&out, "view")
	outstr = out.String()
	t.Log(outstr)
	for k, v := range tpls.Stash {
		if !strings.Contains(outstr, v.(string)) {
			t.Errorf("output does not contain expected value for '%s': %s", k, v)
		}
	}

	// Delete from t.compiled to load it from disk so this corner is covered too.
	tpls.compiled.delete(tpls.toFullPath("view"))
	out.Reset()
	_, _ = tpls.Execute(&out, "view")
	outstr = out.String()
	t.Log(outstr)
	for k, v := range tpls.Stash {
		if !strings.Contains(outstr, v.(string)) {
			t.Errorf("output does not contain expected value for '%s': %s", k, v)
		}
	}
}

func otherBooks(tpls *Gledki) TagFunc {
	setVerboseLoggerOnce()
	return TagFunc(func(w io.Writer, _ string) (int, error) {
		// for more complex file, containing wrapper and include directives, you
		// must use tpls.Compile("path/to/file")
		template := tpls.MustLoadFile("partials/_book_item")
		booksBB := bytes.NewBuffer([]byte(""))
		booksFromDataBase := []map[string]any{
			{"book_title": "Лечителката и рунтавата ѝ… котка", "book_author": "Контадин Кременски"},
			{"book_title": "На пост", "book_author": "Николай Фенерски"},
		}
		for _, book := range booksFromDataBase {
			if _, err := tpls.FtExecStd(template, booksBB, book); err != nil {
				return 0, fmt.Errorf("Problem rendering partial template `_book_item` in 'other_books' TagFunc: %s", err.Error())
			}
		}
		return w.Write(booksBB.Bytes())
	})
}

func TestAddExecuteFunc(t *testing.T) {
	setVerboseLoggerOnce()
	tpls, _ := New(includePaths, filesExt, tagsPair, false)
	tpls.Logger = logger

	tpls.Stash = Stash{
		"a": "a value",
		"b": "b value",
	}
	// ...
	// Later in a galaxy far away
	// ....
	// Prepare a book for display and prepare a list of other books
	tpls.MergeStash(map[string]any{
		"lang":       "en",
		"generator":  "Gledki",
		"included":   "вложена",
		"book_title": "Историософия", "book_author": "Николай Гочев",
		"book_isbn": "9786199169056", "book_issuer": "Студио Беров",
	})
	tpls.Stash["title"] = tpls.Stash["book_title"]
	// Prepare a function for rendering other books.
	// Its result should replace "other_books".
	tpls.Stash["other_books"] = otherBooks(tpls)
	var out strings.Builder
	// Even later, when the whole page is put together
	_, err := tpls.Execute(&out, "book")
	if err != nil {
		t.Errorf("Error executing Gledki.Execute: %s", err.Error())
	}
	if strings.Contains(out.String(), `<div class="book">`) {
		t.Log("Expected content")
	} else {
		t.Errorf("Expected content was not found:\n%s", out.String())
	}
}

// We put the second path as first, so files in it will be found first, if they
// exist. The test is successful if the "black" theme files are used.
func TestAddExecuteFuncWithTheme(t *testing.T) {
	setVerboseLoggerOnce()
	roots := []string{includePaths[1], includePaths[0]}
	tpls, _ := New(roots, filesExt, tagsPair, false)
	tpls.Logger = logger

	tpls.Stash = Stash{
		"a": "стойност за а",
		"b": "стойност за Б",
	}
	// Prepare a book for display and prepare a list of other books
	tpls.MergeStash(map[string]any{
		"lang":       "bg",
		"generator":  "Гледки",
		"included":   "вложена",
		"book_title": "Историософия", "book_author": "Николай Гочев",
		"book_isbn": "9786199169056", "book_issuer": "Студио Беров",
	})
	tpls.Stash["title"] = tpls.Stash["book_title"]
	// Prepare a function for rendering other books.
	// Its result should replace "other_books".
	tpls.Stash["other_books"] = otherBooks(tpls)
	var out strings.Builder
	// Even later, when the whole page is put together
	_, err := tpls.Execute(&out, "book")
	if err != nil {
		t.Errorf("Error executing Gledki.Execute: %s", err.Error())
	}
	outStr := out.String()
	if strings.Contains(outStr, `<div class="black book">`) {
		t.Log("Expected 'black' class")
	} else {
		t.Errorf("Expected class 'black' was not found:\n%s", outStr)
	}
	if strings.Contains(outStr, `<title>black`) {
		t.Log("Expected 'black' title")
	} else {
		t.Errorf("Expected 'black' title was not found:\n%s", outStr)
	}
	// t.Log(outStr)

}

func TestIncludeLimitPanic(t *testing.T) {
	setVerboseLoggerOnce()
	tpls, _ := New(includePaths, filesExt, tagsPair, false)
	tpls.Stash = Stash{
		"title":     "Possibly recursive inclusions",
		"generator": "Gledki",
		"included":  "included",
	}
	level := 0
	tpls.Stash["level"] = TagFunc(func(w io.Writer, _ string) (int, error) {
		level++
		return w.Write([]byte(spf("%d", level)))
	})
	var out strings.Builder
	expectPanic(t, func() { _, _ = tpls.Execute(&out, "includes.htm") })
}

func TestOtherPanics(t *testing.T) {
	setVerboseLoggerOnce()
	tpls, _ := New(includePaths, filesExt, tagsPair, false)
	path := "/ff/a.htm"
	tpls.compiled.store(path, "bla")
	expectPanic(t, func() { tpls.storeCompiled(path, "bla") })
	expectPanic(t, func() { tpls.MustLoadFile(path) })
	expectPanic(t, func() { Must([]string{"/aaa/bbb"}, filesExt, tagsPair, false) })
}

func TestIncludeLimitNoPanic(t *testing.T) {
	setVerboseLoggerOnce()
	tpls, _ := New(includePaths, filesExt, tagsPair, false)

	tpls.Stash = Stash{
		"title":     "Possibly recursive inclusions",
		"generator": "Gledki",
		"included":  "included",
	}
	level := 0
	tpls.Stash["level"] = TagFunc(func(w io.Writer, _ string) (int, error) {
		level++
		return w.Write([]byte(spf("%d", level)))
	})

	tpls.IncludeLimit = 7
	level = 0
	out.Reset()
	_, err := tpls.Execute(&out, "includes")
	if err != nil {
		t.Errorf("Error executing Gledki.Execute: %s", err.Error())
	}
	outstr := out.String()
	t.Log(outstr)

	if !strings.Contains(outstr, "4 4") {
		t.Errorf("output does not contain expected value 4 4")
	}
}

func TestFtExecString(t *testing.T) {
	setVerboseLoggerOnce()
	tpls, _ := New(includePaths, filesExt, tagsPair, false)
	partial := `<div class="pager">${prev}${next}</div>`
	out := tpls.FtExecString(partial, Stash{`prev`: `previous`})
	if strings.Contains(out, "next") {
		t.Error("String should not contain unused placeholder 'next'!")
	}
}

func TestErrors(t *testing.T) {
	setVerboseLoggerOnce()
	if _, err := New([]string{"/ala/bala/nica"}, filesExt, tagsPair, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Logf("Right error: %v", err)
		} else {
			t.Errorf("Wrong error: %v", err)
		}
	} else {
		t.Error("No error - this is unexpected!")
	}
	tpls, _ := New([]string{includePaths[0] + "/../tpls_bad"}, filesExt, tagsPair, false)
	tpls.Logger = logger
	out.Reset()
	if _, err := tpls.Execute(&out, "no_wrapper"); err != nil {
		errstr := err.Error()
		if errors.Is(err, os.ErrNotExist) {
			t.Logf("Right error: %s", errstr)
		} else {
			t.Errorf(`Wrong error: %s`, errstr)
		}
	} else {
		t.Error("No error - this is unexpected!")
	}

	out.Reset()
	if _, err := tpls.Execute(&out, "nosuchfile"); err != nil {
		errstr := err.Error()
		if errors.Is(err, os.ErrNotExist) {
			t.Logf("Right error: %s", errstr)
		} else {
			t.Errorf("Wrong error: %s", errstr)
		}
	} else {
		t.Error("No error - this is unexpected!")
	}

	out.Reset()
	if _, err := tpls.Execute(&out, "no_include"); err != nil {
		errstr := err.Error()
		if errors.Is(err, os.ErrNotExist) {
			t.Logf("Right error: %s", errstr)
		} else {
			t.Errorf("Wrong error: %s", errstr)
		}
	} else {
		t.Errorf("No error - this is unexpected! Output: %s", out.String())
	}
	out.Reset()
	if _, err := tpls.Execute(&out, "incl_no_wrapper.htm"); err != nil {
		errstr := err.Error()
		if errors.Is(err, os.ErrNotExist) {
			t.Logf("Right error: %s", errstr)
		} else {
			t.Errorf("Wrong error: %s", errstr)
		}
	} else {
		t.Errorf("No error - this is unexpected! Output: %s", out.String())
	}

	out.Reset()
	if _, err := tpls.Execute(&out, "incl_no_include.htm"); err != nil {
		errstr := err.Error()
		if errors.Is(err, os.ErrNotExist) {
			t.Logf("Right error: %s", errstr)
		} else {
			t.Errorf("Wrong error:%s", errstr)
		}
	} else {
		t.Errorf("No error - this is unexpected! Output: %s", out.String())
	}

	absRoot, err := filepath.Abs(includePaths[0])
	if err != nil {
		t.Errorf("Error finding absolute path: %s", err.Error())
	}
	_ = tpls.findRoots([]string{absRoot})
	if tpls.Roots[0] == absRoot {
		t.Logf("Right root: %s", tpls.Roots)
	} else {
		t.Logf("Wrong root: Got: %s\n Expected: %s", tpls.Roots[0], absRoot)
	}

	if err = tpls.findRoots([]string{"../ala/bala"}); err != nil {
		errstr := err.Error()
		if errors.Is(err, os.ErrNotExist) {
			t.Logf("Right error: %s", errstr)
		} else {
			t.Errorf("Wrong error:%s", errstr)
		}
	}
}

func expectPanic(t *testing.T, f func()) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MISSING PANIC")
		} else {
			t.Log(r)
		}
	}()
	f()
}

func TestToFullPathRemembers(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "card"+filesExt)
	if err := os.WriteFile(file, []byte("card"), 0o600); err != nil {
		t.Fatal(err)
	}
	tpls, err := New([]string{root}, filesExt, tagsPair, false)
	if err != nil {
		t.Fatal(err)
	}

	if got := tpls.toFullPath("card"); got != file {
		t.Fatalf("first call: want %q, got %q", file, got)
	}
	// The file is removed from disk, but its full path was remembered, so
	// the disk is not checked again.
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if got := tpls.toFullPath("card"); got != file {
		t.Fatalf("remembered: want %q, got %q", file, got)
	}

	// A path, for which no file was found, is not remembered: a file, created
	// later, is found.
	later := filepath.Join(root, "later"+filesExt)
	if got := tpls.toFullPath("later"); got != "later"+filesExt {
		t.Fatalf("missing file: want %q, got %q", "later"+filesExt, got)
	}
	if err = os.WriteFile(later, []byte("later"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := tpls.toFullPath("later"); got != later {
		t.Fatalf("created later: want %q, got %q", later, got)
	}

	// With CacheTemplates false nothing is remembered.
	CacheTemplates = false
	t.Cleanup(func() { CacheTemplates = true })
	nocache := filepath.Join(root, "nocache"+filesExt)
	if err = os.WriteFile(nocache, []byte("nocache"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := tpls.toFullPath("nocache"); got != nocache {
		t.Fatalf("no cache: want %q, got %q", nocache, got)
	}
	if err = os.Remove(nocache); err != nil {
		t.Fatal(err)
	}
	if got := tpls.toFullPath("nocache"); got != "nocache"+filesExt {
		t.Fatalf("no cache, removed: want %q, got %q", "nocache"+filesExt, got)
	}
}

func TestFtExec(t *testing.T) {
	tpls, err := New(includePaths, filesExt, tagsPair, false)
	if err != nil {
		t.Fatal(err)
	}
	partial := `<div class="pager">${prev}${next}</div>`
	var buf bytes.Buffer
	n, err := tpls.FtExec(partial, &buf, Stash{`prev`: `previous`})
	if err != nil {
		t.Fatal(err)
	}
	want := `<div class="pager">previous</div>`
	if buf.String() != want {
		t.Errorf("want %q, got %q", want, buf.String())
	}
	if n != int64(len(want)) {
		t.Errorf("want %d bytes written, got %d", len(want), n)
	}
	// The result is the same as from FtExecString.
	if s := tpls.FtExecString(partial, Stash{`prev`: `previous`}); s != want {
		t.Errorf("FtExecString: want %q, got %q", want, s)
	}

	// An error from a TagFunc in the stash is returned, not a panic.
	failing := errors.New("failing TagFunc")
	_, err = tpls.FtExec(partial, &buf, Stash{
		`prev`: TagFunc(func(io.Writer, string) (int, error) { return 0, failing }),
	})
	if !errors.Is(err, failing) {
		t.Errorf("want error %v, got %v", failing, err)
	}
}

// TestCompileConcurrently compiles the same template, which is not compiled
// yet, from many goroutines at the same time, like the first HTTP requests
// after the start of an application do. Run it with -race.
func TestCompileConcurrently(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "card"+filesExt), []byte("<p>${title}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	tpls, err := New([]string{root}, filesExt, tagsPair, false)
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, 20) // every goroutine writes only to its own slot
	errs := make([]error, len(texts))
	var wg sync.WaitGroup
	for i := range texts {
		wg.Go(func() { texts[i], errs[i] = tpls.Compile("card") })
	}
	wg.Wait()
	for i := range texts {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if texts[i] != "<p>${title}</p>" {
			t.Fatalf("goroutine %d: got %q", i, texts[i])
		}
	}
}

// filesCount returns how many entries `f` has.
func filesCount(f *filesMap) (n int) {
	f.m.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// TestCompileCacheTemplates checks that with CacheTemplates true a compiled
// template is taken from the cache, and with CacheTemplates false (during
// development) every change in the template files is seen at once: no
// compiled template is taken from memory or from a .htmc file on disk, and no
// file is taken from memory.
func TestCompileCacheTemplates(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compile := func(tpls *Gledki) string {
		t.Helper()
		text, err := tpls.Compile("page")
		if err != nil {
			t.Fatal(err)
		}
		return text
	}
	write("page"+filesExt, "A ${include part}")
	write("part"+filesExt, "1")

	t.Run("cached", func(t *testing.T) {
		tpls, err := New([]string{root}, filesExt, tagsPair, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := compile(tpls); got != "A 1" {
			t.Fatalf("first compile: got %q", got)
		}
		write("part"+filesExt, "2")
		if got := compile(tpls); got != "A 1" {
			t.Fatalf("the compiled template must be taken from the cache, got %q", got)
		}
	})

	t.Run("not cached", func(t *testing.T) {
		CacheTemplates = false
		t.Cleanup(func() { CacheTemplates = true })
		// A compiled file, left from a run with CacheTemplates true.
		write("page"+filesExt+CompiledSuffix, "STALE")
		write("part"+filesExt, "1")
		tpls, err := New([]string{root}, filesExt, tagsPair, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := compile(tpls); got != "A 1" {
			t.Fatalf("the .htmc file on disk must not be used, got %q", got)
		}
		write("part"+filesExt, "2")
		if got := compile(tpls); got != "A 2" {
			t.Fatalf("a change in an included file must be seen at once, got %q", got)
		}
		write("page"+filesExt, "B ${include part}")
		if got := compile(tpls); got != "B 2" {
			t.Fatalf("a change in the template must be seen at once, got %q", got)
		}
	})
}
