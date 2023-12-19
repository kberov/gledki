package tmpls

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

var templatesDir = "./testdata/tpls"
var filesExt = ".htm"
var logger *log.Logger
var tagsPair = [2]string{"${", "}"}
var out strings.Builder

// remove all compiled previously templates
func init() {
	sfx := filesExt + compiledSufix
	filepath.WalkDir(templatesDir, func(path string, d fs.DirEntry, err error) error {
		if strings.HasSuffix(path, sfx) {
			os.Remove(path)
		}
		return err
	})
	var lgbuf = bytes.NewBuffer([]byte(""))
	logger = log.New("tmpls")
	logger.SetOutput(lgbuf)
	logger.SetLevel(log.DEBUG)
	logger.SetHeader(defaultLogHeader)
}

func TestNew(t *testing.T) {
	// load templates
	tpls, err := New(templatesDir, filesExt, tagsPair, true)
	tpls.Logger = logger
	if err != nil {
		t.Fatal("Error New: ", err.Error())
	} else {
		t.Logf("\ntmpls.New loads all files in %s", templatesDir)
		for k := range tpls.files {
			_ = k
			//	t.Logf("file: %s", k)
		}
	}
	// do not load templates
	tpls, err = New(templatesDir, filesExt, tagsPair, false)
	tpls.Logger = logger
	if err != nil {
		t.Fatal("Eror New: ", err.Error())
	}
	if len(tpls.files) > 0 {
		t.Fatal("templates should not be loaded")
	}
	//Try to load nonreadable templates
	os.Chmod(templatesDir+"/../tpls_bad/_noread.htm", 0300)
	_, err = New(templatesDir+"/../tpls_bad", filesExt, tagsPair, true)
	if err != nil {
		t.Logf("Expected error from New: %s", err.Error())
		os.Chmod(templatesDir+"/../tpls_bad/_noread.htm", 0400)
	} else {
		t.Fatal("Reading nonreadable file should have failed!")
	}
}

func ExampleNew() {
	tpls, _ := New(templatesDir, filesExt, tagsPair, false)
	fmt.Printf(`A Tmpls object properties:
	Stash: %#v
	Ext: %#v
	root: %s
	Tags: %#v
	IncludeLimit: %d
	Logger: %T from "github.com/labstack/gommon/log"
`, tpls.Stash, tpls.Ext, tpls.root,
		tpls.Tags, tpls.IncludeLimit, tpls.Logger)

	// Output:
	// A Tmpls object properties:
	//	Stash: tmpls.Stash{}
	//	Ext: ".htm"
	//	root: /home/berov/opt/dev/tmpls/testdata/tpls
	//	Tags: [2]string{"${", "}"}
	//	IncludeLimit: 3
	//	Logger: *log.Logger from "github.com/labstack/gommon/log"

	// New may return various errors
	if _, err := New("/ala/bala", filesExt, tagsPair, false); err != nil {
		fmt.Println(err.Error())
	}

	// Tmpls root directory '/ala/bala' does not exist!

}

func ExampleNew_err() {

	// New may return various errors
	if _, err := New("/ala/bala", filesExt, tagsPair, false); err != nil {
		fmt.Println(err.Error())
	}

	// Output:
	// Tmpls root directory '/ala/bala' does not exist!

}

var data = Stash{
	"title":     "Здрасти",
	"body":      "<p>Едно тяло тук</p>",
	"lang":      "bg",
	"generator": "Образци",
	"included":  "вложена",
}

func TestExecute(t *testing.T) {
	tpls, _ := New(templatesDir, filesExt, tagsPair, false)
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
		"generator": "Tmpls",
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

func ExampleTmpls_Execute_simple() {

	// Once on startup. Notice that we use now "<%" and "%>" as tags . "${" and
	// "}" will stay.
	tpls, _ := New(templatesDir, filesExt, [2]string{"<%", "%>"}, false)
	tpls.Logger.SetLevel(log.DEBUG)
	// ...
	// Later... many times and with various data
	tpls.Stash = map[string]any{"generator": "Изгледи"}
	tpls.MergeStash(map[string]any{
		"title": "Hello",
		"body": TagFunc(func(w io.Writer, tag string) (int, error) {
			// very powerful...
			tpls.Stash["generator"] = "Something"
			return w.Write([]byte("Some complex callculations to construct the body."))
		}),
	})

	// Even later - many times - on each response
	tpls.Execute(os.Stdout, "simple")
	// Output:
	// <!doctype html>
	// <html lang="">
	// <head>
	// <meta charset="UTF-8">
	// <meta name="generator" content="Изгледи">
	// <title>Hello</title>
	// </head>
	// <body>
	//
	// <header><h1>Hello</h1></header>
	// <h1>Hello</h1>
	// <section>Some complex callculations to construct the body. Changed generator to "Something"</section>
	// <footer>Тази страница бе създадена с ${generator} и ${included}.</footer>
	//
	//
	// </body>
	// </html>
}

func TestAddExecuteFunc(t *testing.T) {

	tpls, _ := New(templatesDir, filesExt, tagsPair, false)
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
		"generator":  "Tmpls",
		"included":   "вложена",
		"book_title": "Историософия", "book_author": "Николай Гочев",
		"book_isbn": "9786199169056", "book_issuer": "Студио Беров",
	})
	// Prepare a function for rendering other books
	tpls.Stash["other_books"] = TagFunc(func(w io.Writer, tag string) (int, error) {
		// for more complex file, containing wrapper and include directives, you
		// must use tpls.Compile("path/to/file")
		template, err := tpls.LoadFile("partials/_book_item")

		if err != nil {
			return 0, fmt.Errorf(
				"Problem loading partial template `_book_item` in 'other_books' TagFunc: %s", err.Error())
		}
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

	// Even later, when the whole page is put together
	_, err := tpls.Execute(os.Stdout, "book")
	if err != nil {
		t.Fatalf("Error executing Tmpls.Execute: %s", err.Error())
	}
}

func TestIncludeLimitPanic(t *testing.T) {
	tpls, _ := New(templatesDir, filesExt, tagsPair, false)
	tpls.Stash = Stash{
		"title":     "Possibly recursive inclusions",
		"generator": "Tmpls",
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

	tpls, _ := New(templatesDir, filesExt, tagsPair, false)
	path := "/ff/a.htm"
	tpls.compiled[path] = "bla"
	tpls.wg.Add(1)
	expectPanic(t, func() { tpls.storeCompiled(path, tpls.compiled[path]) })
}

func TestIncludeLimitNoPanic(t *testing.T) {
	tpls, _ := New(templatesDir, filesExt, tagsPair, false)

	tpls.Stash = Stash{
		"title":     "Possibly recursive inclusions",
		"generator": "Tmpls",
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
		t.Fatalf("Error executing Tmpls.Execute: %s", err.Error())
	}
	outstr := out.String()
	t.Log(outstr)

	if !strings.Contains(outstr, "4 4") {
		t.Fatalf("output does not contain expected value 4 4")
	}
}

func TestErrors(t *testing.T) {

	if _, err := New("/ala/bala/nica", filesExt, tagsPair, false); err != nil {
		errstr := err.Error()
		if strings.Contains(errstr, "does not exist") {
			t.Logf("Right error: %s", err.Error())
		} else {
			t.Fatalf("Wrong error: errstr")
		}
	} else {
		t.Fatal("No error - this is unexpected!")
	}
	tpls, _ := New(templatesDir+"/../tpls_bad", filesExt, tagsPair, false)
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

	absRoot, err := filepath.Abs(templatesDir)
	if err != nil {
		t.Fatalf("Error finding absolute path: %s", err.Error())
	}
	_ = tpls.findRoot(absRoot)
	if tpls.root == absRoot {
		t.Logf("Right root: %s", tpls.root)
	} else {
		t.Logf("Wrong root: Got: %s\n Expected: %s", tpls.root, absRoot)
	}

	if err = tpls.findRoot("../ala/bala"); err != nil {
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
