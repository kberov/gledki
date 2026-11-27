package gledki

import (
	"fmt"
	"io"

	"github.com/valyala/fasttemplate"
)

// Exec takes the compiled template of the template file `path` (making it
// first, if needed – see [Gledki.Compile]) and executes its parsed template
// with [fasttemplate.Template.ExecuteFunc] and the values from `stash`. The
// compiled text is parsed only once, when the compiled template is made, so
// executing it only writes the output to `w`. String values are not copied,
// when `w` has a WriteString method: they are written with [io.WriteString].
//
// For a tag, whose key is not in `stash`, Exec runs the ready call of a
// builtin, if the tag is one (see Calls in the package documentation).
//
// Unlike [Gledki.Execute], Exec does not use, change or clear [Gledki.Stash].
// So one Gledki can be used by many goroutines at the same time (for example
// one per HTTP request), when each of them passes its own `stash`. Each must
// pass its own, also because the template may write `stash`: the builtin Set
// and the functions of the user (see [Func]) write in it, so one stash, shared
// by executions at the same time, is a data race. A [TagFunc] in `stash`,
// which needs other values while the template is executed, can take them from
// the same `stash`.
//
// A start tag without an end tag is written as it is (see [Gledki.Compile]).
//
// Exec panics, when:
//   - a value in `stash` is not a []byte, a string or a [TagFunc];
//   - [Gledki.IncludeLimit] is reached while compiling or by nested calls of
//     the builtin Exec;
//   - a condition of If, IfElse or Unless is not a bool;
//   - a path of Exec or a key of Set is not a string;
//   - an argument of a function of the user (see [Func]) has the wrong type;
//   - Set is called with a nil `stash`.
//
// The path is resolved like in [Gledki.Execute]. In v0.8.0 Exec becomes
// Execute: Execute will take the stash as an argument and Exec will be
// removed.
func (t *Gledki) Exec(w io.Writer, path string, stash Stash) (int64, error) {
	return t.exec(w, path, stash, nil)
}

// exec does the work of [Gledki.Exec]. `top` is the execution of the
// Gledki.Exec, which runs the builtin Exec calls (nil for Gledki.Exec itself:
// then the new execution is its own top).
// For every tag it writes the value from `stash`, when `stash` has the key
// (even with a nil value); otherwise it runs the ready call of a builtin, if
// the tag is one (see [call.run]); otherwise it writes nothing.
func (t *Gledki) exec(w io.Writer, path string, stash Stash, top *execution) (int64, error) {
	compiledT, err := t.compile(path)
	if err != nil {
		return 0, err
	}
	e := &execution{t: t, stash: stash, top: top}
	if top == nil {
		e.top = e
	}
	f := func(w io.Writer, tag string) (int, error) {
		if v, ok := stash[tag]; ok {
			return writeValue(w, tag, v)
		}
		if c := compiledT.calls[tag]; c != nil {
			return runIn(path, c, e, w)
		}
		return 0, nil
	}
	if compiledT.tpl == nil {
		// A start tag without an end tag: it is written as it is, as in v0.7.8.
		return fasttemplate.ExecuteFunc(compiledT.text, t.Tags[0], t.Tags[1], w, f)
	}
	return compiledT.tpl.ExecuteFunc(w, f)
}

// runIn runs the call `c` of the template `path` in the execution `e`. A
// panic of the call gets the name of the template in front of its message,
// so a nested Exec shows the whole chain:
//
//	template "page": template "partials/form": If(u, `x`): argument 1 (u) is <nil>, not bool
func runIn(path string, c *call, e *execution, w io.Writer) (int, error) {
	defer func() {
		if r := recover(); r != nil {
			panic(fmt.Sprintf("template %q: %v", path, r))
		}
	}()
	return c.run(e, w)
}
