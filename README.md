# Gledki (Гледки)
Package gledki provides a templates, data manager and preprocessor for
[fasttemplate](https://github.com/valyala/fasttemplate)

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

// Once on startup. One Gledki for the whole application.
tpls, err := gl.New(templatesRoots, filesExt, [2]string{"<%", "%>"}, false)
if err != nil {
	fmt.Print("Error:", err.Error())
	os.Exit(1)
}
tpls.Logger.SetLevel(log.DEBUG)
// …
// Later… many times, for example in every HTTP request, each in its own
// goroutine and with its own stash (string, []byte, gledki.TagFunc).
stash := gl.Stash{"generator": "Гледки", "title": "Hello"}
stash["body"] = gl.TagFunc(func(w io.Writer, _ string) (int, error) {
	// Entries of the stash of this execution can be changed from within
	// a TagFunc.
	stash["generator"] = "Something"
	return w.Write([]byte("<p>Some complex callculations to construct the body.</p>"))
})

// See used templates in testdata/tpls.
tpls.Exec(os.Stdout, "simple", stash)
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

See other examples in examples_test.go and examples_calls_test.go.

`Exec` does not use the shared `Gledki.Stash`, so one Gledki can be used by
many goroutines at the same time. `Gledki.Stash`, `MergeStash` and `Execute`
are deprecated: in v0.8.0 `Execute` takes the stash as an argument, like
`Exec`, and `Gledki.Stash`, `MergeStash` and `Exec` are removed.

## Calls and functions

A template can call a few functions with values from the stash, so that fewer
values have to be prepared in Go:

```html
<img src="${IfElse(signedIn, `/img/user.svg`, `/img/users-plus.svg`)}">
<p>${Unless(signedIn, `Please sign in.`)}</p>
${If(showForm, Exec(`partials/form`))}
${Set(`title`, `Books`)}${Exec(`partials/head`)}
```

- `If`, `IfElse` and `Unless` write one of their arguments; the condition must
  be a bool in the stash. A template cannot negate a condition, so use
  `Unless(a, …)` instead of `If(!a, …)`.
- `Exec` executes another template file with the same stash.
- `Set` writes a value in the stash; the tags after it and the templates
  executed with `Exec` after it see the value.
- An argument is a stash key, a string literal or a nested call. A nested call
  runs only when it is written, so above the form is executed only when
  `showForm` is true.

The calls are parsed once, when the template is compiled; a wrong call is a
compile error with the file and the line. A template sees only the stash and
the registered functions: it cannot reach anything else in the program.

The program can register its own functions in `init()`:

```go
func init() {
	gl.Func("Selected", gl.StashFunc2(selected))
}

// selected returns "selected", when the value of key in the stash is want.
func selected(s gl.Stash, key, want string) string {
	if fmt.Sprint(s[key]) == want {
		return "selected"
	}
	return ""
}
```

```html
<option ${Selected(`currency`, `BGN`)}>BGN</option>
```

The function gets the stash of the execution first and may also write to it,
so a stash must not be shared by executions, which run at the same time.

## Speed

`BenchmarkCompare` in gledki_compare_test.go executes the same pages with
Gledki, text/template and html/template (i5-6300U, Go 1.26.4):

| page                      | Gledki             | text/template       | html/template        |
|---------------------------|--------------------|---------------------|----------------------|
| 20 values                 | 0.78µs, 1 alloc    | 6.4µs, 42 allocs    | 21.8µs, 142 allocs   |
| 20 choices (`IfElse`)     | 2.4µs, 2 allocs    | 4.9µs, 42 allocs    | 4.8µs, 42 allocs     |
| 20 calls of a function    | 3.6µs, 2 allocs    | 21.3µs, 102 allocs  | 38.6µs, 182 allocs   |
| 5 parts with `Exec`       | 1.7µs, 7 allocs    | 3.0µs, 22 allocs    | 6.9µs, 47 allocs     |
| 5 parts with `include`    | 0.32µs, 1 alloc    | 3.0µs, 22 allocs    | 6.8µs, 47 allocs     |

```sh
go test -run XXX -bench Compare -count 6 . | tee compare.txt
benchstat -col /engine compare.txt
```

html/template escapes every value for HTML; Gledki and text/template do not.
With Gledki, escape the values in Go, before they go into the stash.

# TODO (May be some day...)

* Think of ways to allow usage of embedded filesystems.
