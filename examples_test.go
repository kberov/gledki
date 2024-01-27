package gledki_test

import (
	"fmt"
	"io"
	"os"

	gl "github.com/kberov/gledki"
	"github.com/labstack/gommon/log"
)

var templatesRootDir = "testdata/tpls"
var filesExt = ".htm"
var logger *log.Logger
var tagsPair = [2]string{"${", "}"}

//var out strings.Builder

func Example_New() {
	tpls, err := gl.New(templatesRootDir, filesExt, tagsPair, false)
	if err != nil {
		fmt.Print("Error:", err.Error())
		os.Exit(1)
	}
	fmt.Printf(`A gledki object properties:
	Stash: %#v
	Ext: %#v
	Tags: %#v
	IncludeLimit: %d (default)
	Logger: %T from "github.com/labstack/gommon/log"
`, tpls.Stash, tpls.Ext,
		tpls.Tags, tpls.IncludeLimit, tpls.Logger)
	// Output:
	// A gledki object properties:
	//	Stash: gledki.Stash{}
	//	Ext: ".htm"
	//	Tags: [2]string{"${", "}"}
	//	IncludeLimit: 3 (default)
	//	Logger: *log.Logger from "github.com/labstack/gommon/log"
}

func Example_New_err() {
	// New may return various errors
	if _, err := gl.New("/ala/bala", filesExt, tagsPair, false); err != nil {
		fmt.Println(err.Error())
	}
	// Output:
	// Gledki root directory '/ala/bala' does not exist!
}

func ExampleGledki_Execute_simple() {

	// Once on startup.
	tpls, err := gl.New(templatesRootDir, filesExt, [2]string{"<%", "%>"}, false)
	if err != nil {
		fmt.Print("Error:", err.Error())
		//os.Exit(1)
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
}
