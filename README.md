# Gledki (Гледки)
Package gledki provides a templates and data manager for [fasttemplate](https://github.com/valyala/fasttemplate)

Because fasttemplate is minimalisitic, the need for this wrapper arose. Two
template directives were implemented – `wrapper` and `include`. They make
`gledki` powerful enough for use in big and complex web applications.

The main template (the one which partial path you pass as argument to
`Gledki.Execute(...)`) can be compiled from several files – as many as you need –
with the simple approach of wrapping and including partial files recursively.
`TagFunc(...)` allows us to keep logic into our Go code and prepare pieces of the
output as needed. Leveraging cleverly TagFunc gives us complete separation of
concerns. This simple but powerful technic made me write this wrapper.
Ah, and „gledki(гледки)“ means "views" in Bulgarian.

See the tests and sample templates for usage examples.

## Note!
This is my first module in Go, so I would be glad to get advices for
improvements, exspecially for idiomatic Go.

## Example usage

```go

import (
	"fmt"
	"io"
	"os"

	gl "github.com/kberov/gledki"
	"github.com/labstack/gommon/log"
)

// Multiple templates paths. The first found template with a certain name is
// loaded. Convenient for themes, multidomain sites etc.
var templatesRoots = []string{"./testdata/tpls/theme","./testdata/tpls" }
var filesExt = ".htm"

//...

// Once on startup.
tpls, err := gl.New(templatesRoots, filesExt, [2]string{"<%", "%>"}, false)
if err != nil {
	fmt.Print("Error:", err.Error())
	os.Exit(1)
}
tpls.Logger.SetLevel(log.DEBUG)
// …
// Later… many times and with various data (string, []byte, gledki.TagFunc)
tpls.Stash = map[string]any{"generator": "Гледки"}

// Somwhere else in your program…
tpls.MergeStash(gl.Stash{
	"title": "Hello",
	"body": gl.TagFunc(func(w io.Writer, tag string) (int, error) {
		// tmpls.Stash entries and even the entire Stash can be modified
		// from within tmpls.TagFunc
		tpls.Stash["generator"] = "Something"
		return w.Write([]byte("<p>Some complex callculations to construct the body.</p>"))
	}),
})

// Even later…
// See used templates in testdata/tpls.
tpls.Execute(os.Stdout, "simple")
// Output:
// <!doctype html>
// <html>
//     <head>
//         <meta charset="UTF-8">
//         <meta name="generator" content="Гледки">
//         <title>Hello</title>
//     </head>
//     <body>
//         <header><h1>Hello</h1></header>
//         <h1>Hello</h1>
//         <section>
//             <p>Some complex callculations to construct the body.</p>
//             <p>Changed generator to "Something".</p>
//         </section>
//         <footer>Тази страница бе създадена с Something.</footer>
//     </body>
// </html>
```

See other examples in gledki_test.go.
