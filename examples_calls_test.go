package gledki_test

import (
	"fmt"
	"os"

	gl "github.com/kberov/gledki"
)

// init registers the function Selected for the templates, as a program
// does: in init(), before any template is compiled. See [ExampleFunc].
func init() {
	gl.Func("Selected", gl.StashFunc2(selected))
}

// selected returns "selected", when the value of `key` in the stash is
// `want`.
func selected(s gl.Stash, key, want string) string {
	if fmt.Sprint(s[key]) == want {
		return "selected"
	}
	return ""
}

// newExamples returns the [gl.Gledki] of the examples of calls. Their
// templates are in testdata/tpls/examples.
func newExamples() *gl.Gledki {
	tpls, err := gl.New(Roots, filesExt, tagsPair, false)
	if err != nil {
		fmt.Print("Error:", err.Error())
		os.Exit(1)
	}
	return tpls
}

func ExampleIf() {
	tpls := newExamples()
	// examples/if.htm:
	// <p>${If(signedIn, `Welcome back!`)}</p>
	for _, signedIn := range []bool{true, false} {
		_, _ = tpls.Exec(os.Stdout, "examples/if", gl.Stash{"signedIn": signedIn})
		fmt.Println()
	}
	// Output:
	// <p>Welcome back!</p>
	// <p></p>
}

func ExampleIfElse() {
	tpls := newExamples()
	// examples/ifelse.htm:
	// <img src="${IfElse(signedIn, `/img/user.svg`, `/img/users-plus.svg`)}">
	for _, signedIn := range []bool{true, false} {
		_, _ = tpls.Exec(os.Stdout, "examples/ifelse", gl.Stash{"signedIn": signedIn})
		fmt.Println()
	}
	// Output:
	// <img src="/img/user.svg">
	// <img src="/img/users-plus.svg">
}

func ExampleUnless() {
	tpls := newExamples()
	// examples/unless.htm:
	// <p>${Unless(signedIn, `Please sign in.`)}</p>
	for _, signedIn := range []bool{true, false} {
		_, _ = tpls.Exec(os.Stdout, "examples/unless", gl.Stash{"signedIn": signedIn})
		fmt.Println()
	}
	// Output:
	// <p></p>
	// <p>Please sign in.</p>
}

// Exec executes another template file with the same stash. Nested in If, it
// runs only when the condition is true.
func Example_exec() {
	tpls := newExamples()
	// examples/exec.htm:
	// <main>${If(showForm, Exec(`examples/form`))}</main>
	// examples/form.htm:
	// <form><h2>${title}</h2></form>
	for _, showForm := range []bool{true, false} {
		_, _ = tpls.Exec(os.Stdout, "examples/exec", gl.Stash{"showForm": showForm, "title": "New book"})
		fmt.Println()
	}
	// Output:
	// <main><form><h2>New book</h2></form></main>
	// <main></main>
}

// Set writes a value in the stash and writes nothing. The tags after it and
// the templates executed with Exec after it see the value.
func Example_set() {
	tpls := newExamples()
	// examples/set.htm:
	// ${Set(`title`, `Books`)}${Exec(`examples/head`)}<h1>${title}</h1>
	// examples/head.htm:
	// <title>${title}</title>
	_, _ = tpls.Exec(os.Stdout, "examples/set", gl.Stash{})
	// Output:
	// <title>Books</title><h1>Books</h1>
}

// Selected is registered in init() of this file:
//
//	func init() {
//		gl.Func("Selected", gl.StashFunc2(selected))
//	}
func ExampleFunc() {
	tpls := newExamples()
	// examples/func.htm:
	// <select name="currency">
	//     <option ${Selected(`currency`, `BGN`)}>BGN</option>
	//     <option ${Selected(`currency`, `EUR`)}>EUR</option>
	// </select>
	_, _ = tpls.Exec(os.Stdout, "examples/func", gl.Stash{"currency": "EUR"})
	// Output:
	// <select name="currency">
	//     <option >BGN</option>
	//     <option selected>EUR</option>
	// </select>
}
