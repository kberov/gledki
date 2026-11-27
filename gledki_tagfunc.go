package gledki

import (
	"fmt"
	"io"
)

// writeValue writes the value `v` of the placeholder `tag` to `w`. A nil
// value writes nothing; a string, a []byte or a [TagFunc] is written; a value
// of any other type panics, as in fasttemplate. [Gledki.Exec] writes every
// value of the stash and every result of a call with it, instead of the
// stdTagFunc of fasttemplate. The only difference is that a string is
// written with [io.WriteString], so when `w` has its own WriteString method
// (like [strings.Builder], [bytes.Buffer], [bufio.Writer],
// [github.com/valyala/bytebufferpool.ByteBuffer] or the http.ResponseWriter of
// net/http), the string is not copied into a new []byte – one allocation less
// for every placeholder.
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
