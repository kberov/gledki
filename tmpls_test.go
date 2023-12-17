package tmpls

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ft "github.com/valyala/fasttemplate"
)

var templates = "./testdata/tpls"
var ext = ".htm"

// remove all compiled previously templates
func init() {
	sfx := ext + "c"
	filepath.WalkDir(templates, func(path string, d fs.DirEntry, err error) error {
		if strings.HasSuffix(path, sfx) {
			os.Remove(path)
		}
		return err
	})
}

func TestNew(t *testing.T) {
	// load templates
	tpls, err := New(templates, ext, [2]string{"${", "}"}, true)
	if err != nil {
		t.Fatal("Error New: ", err.Error())
	} else {
		t.Logf("\ntmpls.New loads all files in %s", templates)
		for k := range tpls.files {
			_ = k
			//	t.Logf("file: %s", k)
		}
	}
	// do not load templates
	tpls, err = New(templates, ext, [2]string{"${", "}"}, false)
	if err != nil {
		t.Fatal("Eror New: ", err.Error())
	}
	if len(tpls.files) > 0 {
		t.Fatal("templates should not be loaded")
	}
	// load nonreadable templates
	_, err = New(templates+"/../tpls_bad", ext, [2]string{"${", "}"}, true)
	if err != nil {
		t.Logf("Expected error from New: %s", err.Error())
	} else {
		t.Fatal("Reading nonreadable file should have failed!")
	}
}

var data = DataMap{
	"title":     "Здрасти",
	"body":      "<p>Едно тяло тук</p>",
	"lang":      "bg",
	"generator": "Образци",
	"included":  "вложена",
}

func TestExecute(t *testing.T) {
	tpls, _ := New(templates, ext, [2]string{"${", "}"}, false)
	tpls.DataMap = data
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
	tpls.DataMap = DataMap{
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
	for k, v := range tpls.DataMap {
		if !strings.Contains(outstr, v.(string)) {
			t.Fatalf("output does not contain expected value for '%s': %s", k, v)
		}
	}
}

func TestAddExecuteFunc(t *testing.T) {

	tpls, _ := New(templates, ext, [2]string{"${", "}"}, false)

	tpls.DataMap = DataMap{
		"a": "a value",
		"b": "b value",
	}
	// ...
	// Later in a galaxy far away
	// ....
	// Prepare a book for display and prepare a list of other books
	tpls.MergeDataMap(map[string]any{
		"lang":       "en",
		"generator":  "Tmpls",
		"included":   "вложена",
		"book_title": "Историософия", "book_author": "Николай Гочев",
		"book_isbn": "9786199169056", "book_issuer": "Студио Беров",
	})
	// Prepare a function for rendering other books
	tpls.DataMap["other_books"] = ft.TagFunc(func(w io.Writer, tag string) (int, error) {
		// for more complex file, containing wrapper and include directives, you
		// must use tpls.Compile("path/to/file")
		template, err := tpls.LoadFile("partials/_book_item")

		if err != nil {
			return 0, fmt.Errorf(
				"Problem loading partial template `_book_item` in 'other_books' TagFunc: %s", err.Error())
		}
		rendered := bytes.NewBuffer([]byte(""))
		booksFromDataBase := []map[string]any{
			{"book_title": "Лечителката и рунтавата ѝ… котка", "book_author": "Контадин Кременски"},
			{"book_title": "На пост", "book_author": "Николай Фенерски"},
		}
		for _, book := range booksFromDataBase {
			if _, err := ft.Execute(template, tpls.Tags[0], tpls.Tags[1], rendered, book); err != nil {
				return 0, fmt.Errorf("Problem rendering partial template `_book_item` in 'other_books' TagFunc: %s", err.Error())
			}
		}
		return w.Write(rendered.Bytes())
	})

	// Even later, when the whole page is put together
	_, err := tpls.Execute(os.Stdout, "book")
	if err != nil {
		log.Fatalf("Error executing Tmpls.Execute: %s", err.Error())
	}
}

func TestIncludeLimitPanic(t *testing.T) {
	tpls, _ := New(templates, ext, [2]string{"${", "}"}, false)
	tpls.DataMap = DataMap{
		"title":     "Possibly recursive inclusions",
		"generator": "Tmpls",
		"included":  "included",
	}
	level := 0
	tpls.DataMap["level"] = ft.TagFunc(func(w io.Writer, tag string) (int, error) {
		level++
		return w.Write([]byte(spf("%d", level)))
	})
	var out strings.Builder
	expectPanic(t, func() { _, _ = tpls.Execute(&out, "includes") })
}

func TestOtherPanics(t *testing.T) {

	tpls, _ := New(templates, ext, [2]string{"${", "}"}, false)
	path := "/ff/a.htm"
	tpls.compiled[path] = "bla"
	tpls.wg.Add(1)
	expectPanic(t, func() { tpls.storeCompiled(path, tpls.compiled[path]) })
	// abs. path
	expectPanic(t, func() { findRoot(path) })
	// rel. path
	expectPanic(t, func() { findRoot("." + path) })
}

func TestIncludeLimitNoPanic(t *testing.T) {
	tpls, _ := New(templates, ext, [2]string{"${", "}"}, false)

	tpls.DataMap = DataMap{
		"title":     "Possibly recursive inclusions",
		"generator": "Tmpls",
		"included":  "included",
	}
	level := 0
	tpls.DataMap["level"] = ft.TagFunc(func(w io.Writer, tag string) (int, error) {
		level++
		return w.Write([]byte(spf("%d", level)))
	})

	tpls.IncludeLimit = 7
	level = 0
	var out strings.Builder
	_, err := tpls.Execute(&out, "includes")
	if err != nil {
		log.Fatalf("Error executing Tmpls.Execute: %s", err.Error())
	}
	outstr := out.String()
	t.Log(outstr)

	if !strings.Contains(outstr, "4 4") {
		t.Fatalf("output does not contain expected value 4 4")
	}
}

func expectPanic(t *testing.T, f func()) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("missing panic")
		} else {
			t.Log(r)
		}
	}()
	f()
}
