package gledki

import (
	"fmt"
	"io"
)

// stashTagFunc writes the value of the placeholder `tag` from `stash` to `w`.
// [Gledki.Exec] (and so [Gledki.Execute], which calls Exec) uses it for every
// placeholder in the parsed template instead of the stdTagFunc of
// fasttemplate. It works like it with one difference: a string value is
// written with [io.WriteString], so when `w` has its own WriteString method
// (like [strings.Builder], [bytes.Buffer], [bufio.Writer],
// [github.com/valyala/bytebufferpool.ByteBuffer] or the http.ResponseWriter of
// net/http), the string is not copied into a new []byte – one allocation less
// for every placeholder.
//
// A missing or nil value writes nothing. A value of a type other than
// []byte, string or [TagFunc] panics, as in fasttemplate.
func stashTagFunc(w io.Writer, tag string, stash Stash) (int, error) {
	return writeValue(w, tag, stash[tag])
}

// writeValue writes the value `v` of the placeholder `tag` to `w`, as
// [stashTagFunc] describes: nil writes nothing; a string, a []byte or a
// [TagFunc] is written; any other type panics.
func writeValue(w io.Writer, tag string, v any) (int, error) {
	switch value := v.(type) {
	case nil:
		return 0, nil
	case string:
		return io.WriteString(w, value)
	case []byte:
		return w.Write(value)
	case TagFunc:
		return value(w, tag)
	default:
		panic(fmt.Sprintf("tag=%q contains unexpected value type=%#v. Expected []byte, string or TagFunc", tag, value))
	}
}
