package gledki

import (
	"fmt"
	"go/token"
	"io"
	"sync/atomic"
)

// Function is a Go function, made ready to be called from templates by one
// of the adapters [StashFunc0], [StashFunc1] or [StashFunc2], and registered
// with [Func]. The zero Function is not a function.
type Function struct{ b *builtin }

// builtinsRead is set, when [parseCall] reads [builtins] for the first time.
// From then on [Func] refuses to register, so builtins is only read while
// many goroutines compile templates, without a lock.
var builtinsRead atomic.Bool

// Func registers the function `fn` under `name` for all templates of all
// [Gledki] instances of the program, so a template can call it, for example
// ${Selected(`currency`, `EUR`)}. Register the functions in init() (or in
// main before the first compile): Func panics, when any template was already
// compiled, when `name` is not a Go identifier or is already registered (also
// the builtins If, IfElse, Unless, Exec and Set), and when `fn` is the zero
// [Function].
//
// The function gets the stash of the current execution as its first
// argument. It may read and write it: a written value is seen by the tags
// after the call and by the templates executed with Exec after it.
func Func(name string, fn Function) {
	switch {
	case fn.b == nil:
		panic(fmt.Sprintf("gledki.Func(%q): no function", name))
	case !token.IsIdentifier(name):
		panic(fmt.Sprintf("gledki.Func(%q): not a Go identifier", name))
	case builtins[name] != nil:
		panic(fmt.Sprintf("gledki.Func(%q): already registered", name))
	case builtinsRead.Load():
		panic(fmt.Sprintf("gledki.Func(%q): templates are already compiled; register functions in init()", name))
	}
	builtins[name] = fn.b
}

// StashFunc0 makes a [Function] from `f`, which takes only the stash; it is
// called in a template without arguments: ${Today()}. The result of `f` is
// written like a stash value (nil, string, []byte or [TagFunc]).
func StashFunc0[R any](f func(Stash) R) Function {
	return Function{&builtin{arity: 0, run: func(e *execution, w io.Writer, c *call, _ []any) (int, error) {
		return writeValue(w, c.text, f(e.stash))
	}}}
}

// StashFunc1 makes a [Function] from `f`, which takes the stash and one
// argument from the template: ${Upper(name)}. The number in the name is the
// count of arguments in the template, without the stash. See [StashFunc0].
func StashFunc1[A, R any](f func(Stash, A) R) Function {
	return Function{&builtin{arity: 1, run: func(e *execution, w io.Writer, c *call, args []any) (int, error) {
		return writeValue(w, c.text, f(e.stash, arg[A](c, args, 0)))
	}}}
}

// StashFunc2 makes a [Function] from `f`, which takes the stash and two
// arguments from the template: ${Selected(`currency`, `EUR`)}. See
// [StashFunc0].
func StashFunc2[A, B, R any](f func(Stash, A, B) R) Function {
	return Function{&builtin{arity: 2, run: func(e *execution, w io.Writer, c *call, args []any) (int, error) {
		return writeValue(w, c.text, f(e.stash, arg[A](c, args, 0), arg[B](c, args, 1)))
	}}}
}
