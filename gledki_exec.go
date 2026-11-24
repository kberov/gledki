package gledki

import "io"

// Exec takes the compiled template of the template file `path` (making it
// first, if needed – see [Gledki.Compile]) and executes its parsed template
// with [fasttemplate.Template.ExecuteFunc] and the values from `stash`. The
// compiled text is parsed only once, when the compiled template is made, so
// executing it only writes the output to `w`. String values are not copied,
// when `w` has a WriteString method (see stashTagFunc in gledki_tagfunc.go).
//
// Unlike [Gledki.Execute], Exec does not use, change or clear [Gledki.Stash].
// So one Gledki can be used by many goroutines at the same time (for example
// one per HTTP request), when each of them passes its own `stash`. A [TagFunc]
// in `stash`, which needs other values while the template is executed, can
// take them from the same `stash`.
//
// The path is resolved like in [Gledki.Execute]. In v0.8.0 Exec becomes
// Execute: Execute will take the stash as an argument and Exec will be
// removed.
func (t *Gledki) Exec(w io.Writer, path string, stash Stash) (int64, error) {
	ct, err := t.compile(path)
	if err != nil {
		return 0, err
	}
	return ct.tpl.ExecuteFunc(w, func(w io.Writer, tag string) (int, error) {
		return stashTagFunc(w, tag, stash)
	})
}
