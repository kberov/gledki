package gledki

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/gommon/log"
)

var includePaths = []string{"./testdata/tpls", "./testdata/tpls/theme"}
var filesExt = ".htm"
var logger *log.Logger
var tagsPair = [2]string{"${", "}"}
var out strings.Builder

// remove all compiled previously templates
func init() {
	sfx := filesExt + CompiledSuffix
	for _, dir := range includePaths {
		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if strings.HasSuffix(path, sfx) {
				os.Remove(path)
			}
			return err
		})
	}
	var lgbuf = bytes.NewBuffer([]byte(""))
	logger = log.New("gledki")
	logger.SetOutput(lgbuf)
	logger.SetLevel(log.DEBUG)
	logger.SetHeader(defaultLogHeader)
}

func TestNew(t *testing.T) {
	// load templates
	tpls, err := New(includePaths, filesExt, tagsPair, true)
	if err != nil {
		t.Fatal("Error New: ", err.Error())
	} else {
		tpls.Logger = logger
		t.Logf("\ngledki.New loads all files in %s", includePaths)
		for k := range tpls.files {
			_ = k
			//	t.Logf("file: %s", k)
		}
	}
	// do not load templates
	tpls, err = New(includePaths, filesExt, tagsPair, false)
	tpls.Logger = logger
	if err != nil {
		t.Fatal("Eror New: ", err.Error())
	}
	if len(tpls.files) > 0 {
		t.Fatal("templates should not be loaded")
	}
	//Try to load nonreadable templates
	os.Chmod(includePaths[0]+"/../tpls_bad/_noread.htm", 0300)
	_, err = New([]string{includePaths[0] + "/../tpls_bad"}, filesExt, tagsPair, true)
	if err != nil {
		t.Logf("Expected error from New: %s", err.Error())
		os.Chmod(includePaths[0]+"/../tpls_bad/_noread.htm", 0400)
	} else {
		t.Fatal("Reading nonreadable file should have failed!")
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
	tpls, _ := New(includePaths, filesExt, tagsPair, false)
	tpls.Logger = logger
	tpls.Stash = data
	var out strings.Builder
	_, _ = tpls.Execute(&out, "view")
	outstr := out.String()
	t.Log(outstr)
	for k, v := range data {
		if !strings.Contains(outstr, v.(string)) {
			t.Fatalf("output does not contain expected value for '%s': %s", k, v)
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
			t.Fatalf("output does not contain expected value for '%s': %s", k, v)
		}
	}

	// Delete from t.compiled to load it from disk so this corner is covered too.
	delete(tpls.compiled, tpls.toFullPath("view"))
	out.Reset()
	_, _ = tpls.Execute(&out, "view")
	outstr = out.String()
	t.Log(outstr)
	for k, v := range tpls.Stash {
		if !strings.Contains(outstr, v.(string)) {
			t.Fatalf("output does not contain expected value for '%s': %s", k, v)
		}
	}
}

func otherBooks(tpls *Gledki) TagFunc {
	return TagFunc(func(w io.Writer, tag string) (int, error) {
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
		t.Fatalf("Error executing Gledki.Execute: %s", err.Error())
	}
	if strings.Contains(out.String(), `<div class="book">`) {
		t.Log("Expected content")
	} else {
		t.Fatalf("Expected content was not found:\n%s", out.String())
	}
}

// We put the second path as first, so files in it will be found first, if they exist.
func TestAddExecuteFuncWithTheme(t *testing.T) {
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
		t.Fatalf("Error executing Gledki.Execute: %s", err.Error())
	}
	outStr := out.String()
	if strings.Contains(outStr, `<div class="black book">`) {
		t.Log("Expected 'black' class")
	} else {
		t.Fatalf("Expected class 'black' was not found:\n%s", outStr)
	}
	if strings.Contains(outStr, `<title>black`) {
		t.Log("Expected 'black' title")
	} else {
		t.Fatalf("Expected 'black' title was not found:\n%s", outStr)
	}
	// t.Log(outStr)

}

func TestIncludeLimitPanic(t *testing.T) {
	tpls, _ := New(includePaths, filesExt, tagsPair, false)
	tpls.Stash = Stash{
		"title":     "Possibly recursive inclusions",
		"generator": "Gledki",
		"included":  "included",
	}
	level := 0
	tpls.Stash["level"] = TagFunc(func(w io.Writer, tag string) (int, error) {
		level++
		return w.Write([]byte(spf("%d", level)))
	})
	var out strings.Builder
	expectPanic(t, func() { _, _ = tpls.Execute(&out, "includes.htm") })
}

func TestOtherPanics(t *testing.T) {

	tpls, _ := New(includePaths, filesExt, tagsPair, false)
	path := "/ff/a.htm"
	tpls.compiled[path] = "bla"
	tpls.wg.Add(1)
	expectPanic(t, func() { tpls.storeCompiled(path, tpls.compiled[path]) })
	expectPanic(t, func() { tpls.MustLoadFile(path) })
	expectPanic(t, func() { Must([]string{"/aaa/bbb"}, filesExt, tagsPair, false) })
}

func TestIncludeLimitNoPanic(t *testing.T) {
	tpls, _ := New(includePaths, filesExt, tagsPair, false)

	tpls.Stash = Stash{
		"title":     "Possibly recursive inclusions",
		"generator": "Gledki",
		"included":  "included",
	}
	level := 0
	tpls.Stash["level"] = TagFunc(func(w io.Writer, tag string) (int, error) {
		level++
		return w.Write([]byte(spf("%d", level)))
	})

	tpls.IncludeLimit = 7
	level = 0
	out.Reset()
	_, err := tpls.Execute(&out, "includes")
	if err != nil {
		t.Fatalf("Error executing Gledki.Execute: %s", err.Error())
	}
	outstr := out.String()
	t.Log(outstr)

	if !strings.Contains(outstr, "4 4") {
		t.Fatalf("output does not contain expected value 4 4")
	}
}

func TestFtExecString(t *testing.T) {
	tpls, _ := New(includePaths, filesExt, tagsPair, false)
	partial := `<div class="pager">${prev}${next}</div>`
	out := tpls.FtExecString(partial, Stash{`prev`: `previous`})
	if strings.Contains(out, "next") {
		t.Fatal("String should not contain unused placeholder 'next'!")
	}
}

func TestErrors(t *testing.T) {

	if _, err := New([]string{"/ala/bala/nica"}, filesExt, tagsPair, false); err != nil {
		errstr := err.Error()
		if strings.Contains(errstr, "does not exist") {
			t.Logf("Right error: %s", err.Error())
		} else {
			t.Fatalf("Wrong error: errstr")
		}
	} else {
		t.Fatal("No error - this is unexpected!")
	}
	tpls, _ := New([]string{includePaths[0] + "/../tpls_bad"}, filesExt, tagsPair, false)
	tpls.Logger = logger
	out.Reset()
	if _, err := tpls.Execute(&out, "no_wrapper"); err != nil {
		errstr := err.Error()
		if strings.Contains(errstr, "could not be read") {
			t.Logf("Right error: %s", err.Error())
		} else {
			t.Fatalf("Wrong error: errstr")
		}
	} else {
		t.Fatal("No error - this is unexpected!")
	}

	out.Reset()
	if _, err := tpls.Execute(&out, "nosuchfile"); err != nil {
		errstr := err.Error()
		if strings.Contains(errstr, "could not be read") {
			t.Logf("Right error: %s", err.Error())
		} else {
			t.Fatalf("Wrong error: errstr")
		}
	} else {
		t.Fatal("No error - this is unexpected!")
	}

	out.Reset()
	if _, err := tpls.Execute(&out, "no_include"); err != nil {
		errstr := err.Error()
		if strings.Contains(errstr, "could not be read") {
			t.Logf("Right error: %s", err.Error())
		} else {
			t.Fatalf("Wrong error:%s", errstr)
		}
	} else {
		t.Fatalf("No error - this is unexpected! Output: %s", out.String())
	}
	out.Reset()
	if _, err := tpls.Execute(&out, "incl_no_wrapper.htm"); err != nil {
		errstr := err.Error()
		if strings.Contains(errstr, "could not be read") {
			t.Logf("Right error: %s", err.Error())
		} else {
			t.Fatalf("Wrong error:%s", errstr)
		}
	} else {
		t.Fatalf("No error - this is unexpected! Output: %s", out.String())
	}

	out.Reset()
	if _, err := tpls.Execute(&out, "incl_no_include.htm"); err != nil {
		errstr := err.Error()
		if strings.Contains(errstr, "could not be read") {
			t.Logf("Right error: %s", err.Error())
		} else {
			t.Fatalf("Wrong error:%s", errstr)
		}
	} else {
		t.Fatalf("No error - this is unexpected! Output: %s", out.String())
	}

	absRoot, err := filepath.Abs(includePaths[0])
	if err != nil {
		t.Fatalf("Error finding absolute path: %s", err.Error())
	}
	_ = tpls.findRoots([]string{absRoot})
	if tpls.Roots[0] == absRoot {
		t.Logf("Right root: %s", tpls.Roots)
	} else {
		t.Logf("Wrong root: Got: %s\n Expected: %s", tpls.Roots[0], absRoot)
	}

	if err = tpls.findRoots([]string{"../ala/bala"}); err != nil {
		errstr := err.Error()
		if strings.Contains(errstr, "does not exist!") {
			t.Logf("Right error: %s", err.Error())
		} else {
			t.Fatalf("Wrong error:%s", errstr)
		}
	}
}

func expectPanic(t *testing.T, f func()) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("MISSING PANIC")
		} else {
			t.Log(r)
		}
	}()
	f()
}
