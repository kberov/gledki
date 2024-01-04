# Gledki (Гледки)
A templates and data manager for [fasttemplate](https://github.com/valyala/fasttemplate)

Package gledki provides a templates and data manager for
[https://github.com/valyala/fasttemplate].

Because fasttemplate is minimalisitic, the need
for this wrapper arose. Two template directives were implemented – `wrapper`
and `include`. These make this simple templates manager powerful enough for
big and complex sites or generating any text output.

The main template can be compiled from several files – as many as you need –
with the simple approach of wrapping and including files recursively.
fasttemplate's TagFunc allows us to keep logic into our Go code and prepare
pieces of the output as needed. See the tests and sample templates for usage
examples.

Gledki is plural for гледка in Bulgarian, which literally means "view". So this
package provides means to implement views.

## Note!
This is my first module in Go, so I would be glad to get advices for
improvements, exspecially for idiomatic Go.

## Example usage

```go

import "github.com/kberov/gledki"
//...
var templates = "./testdata/tpls"
var ext = ".htm"
var logger *log.Logger
var tags = [2]string{"${", "}"}
//...
// Instantiate the templates manager.
tpls, _ := New(templates, ext, tags, false)
tpls.Logger = logger

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
	"generator":  "Gledki",
	"included":   "вложена",
	"book_title": "Историософия", "book_author": "Николай Гочев",
	"book_isbn": "9786199169056", "book_issuer": "Студио Беров",
})
// Prepare a function for rendering other books
tpls.DataMap["other_books"] = TagFunc(func(w io.Writer, tag string) (int, error) {
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
	t.Fatalf("Error executing Gledki.Execute: %s", err.Error())
}

```

See other examples in [gledki_test.go] and [example_test.go]
