package gledki

import (
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
// Unlike [Gledki.Execute], Exec does not use, change or clear [Gledki.Stash].
// So one Gledki can be used by many goroutines at the same time (for example
// one per HTTP request), when each of them passes its own `stash`. A [TagFunc]
// in `stash`, which needs other values while the template is executed, can
// take them from the same `stash`.
//
// A start tag without an end tag is written as it is (see [Gledki.Compile]).
// Exec panics, when a value in `stash` is not a []byte, a string or a
// [TagFunc], and when [Gledki.IncludeLimit] is reached while compiling.
//
// The path is resolved like in [Gledki.Execute]. In v0.8.0 Exec becomes
// Execute: Execute will take the stash as an argument and Exec will be
// removed.
func (t *Gledki) Exec(w io.Writer, path string, stash Stash) (int64, error) {
	compiledT, err := t.compile(path)
	if err != nil {
		return 0, err
	}
	f := func(w io.Writer, tag string) (int, error) {
		return stashTagFunc(w, tag, stash)
	}
	if compiledT.tpl == nil {
		// A start tag without an end tag: it is written as it is, as in v0.7.8.
		return fasttemplate.ExecuteFunc(compiledT.text, t.Tags[0], t.Tags[1], w, f)
	}
	return compiledT.tpl.ExecuteFunc(w, f)
}
