package tmpls

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	ft "github.com/valyala/fasttemplate"
)

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

func ExampleTmpls() {
	tpls, _ := New(templates, ext, [2]string{"${", "}"}, false)
	// If you need deeper recursive inclusion limit
	tpls.IncludeLimit = 5
	//...
}

func ExampleTmpls_Execute() {

	var data = DataMap{
		"title":     "Здрасти",
		"body":      "<p>Едно тяло тук</p>",
		"lang":      "bg",
		"generator": "Образци",
		"included":  "вложена",
	}

	tpls, _ := New(templates, ext, [2]string{"${", "}"}, false)
	tpls.DataMap = data
	var out strings.Builder
	// Compile and execute file ../testdata/tpls/view.htm
	if length, err := tpls.Execute(&out, "view"); err != nil {
		fmt.Printf("Length:%d\nOutput:\n%s", length, out.String())
	} else {
		os.Stderr.Write([]byte("err:" + err.Error()))
	}
}

func ExampleTmpls_LoadFile() {
	tpls, _ := New(templates, ext, [2]string{"${", "}"}, false)

	// Replace some placeholder with static content
	content, err := tpls.LoadFile("partials/_script")
	if err != nil {
		slog.Error(fmt.Sprintf("Problem loading partial template `script` %s", err.Error()))
	}
	tpls.DataMap["script"] = "<script>" + content + "</script>"

	//...

	// Prepare a function for replacing a placeholder
	tpls.DataMap["other_books"] = ft.TagFunc(func(w io.Writer, tag string) (int, error) {
		// for more complex file, containing wrapper and include directives, you
		// can use tpls.Compile("path/to/file") the same way as LoadFile
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
			if _, err := tpls.FtExecStd(template, rendered, book); err != nil {
				return 0, fmt.Errorf(
					"Problem rendering partial template `_book_item` in 'other_books' TagFunc: %s",
					err.Error())
			}
		}
		return w.Write(rendered.Bytes())
	})
}
