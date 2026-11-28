package gledki

import "io"

// IfElse allows chunks of content to be written to the template
// conditionally. If cond is true, then is written, otherwise else is written.
// The choice is made once, when IfElse is called, so the returned [TagFunc]
// only writes the chosen value. The arguments are expected to be nil, []byte,
// string or [TagFunc]. If there is no else part, use [If].
func IfElse(cond bool, then, els any) TagFunc {
	v := els
	if cond {
		v = then
	}
	return value(v)
}

// If allows chunks of content to be written to the template conditionally. If
// accepts only one statement/value argument which it evaluates only if cond
// happens to be true. The argument stmt is expected to be a []byte, string or
// [TagFunc].
func If(cond bool, stmt any) TagFunc {
	if cond {
		return value(stmt)
	}
	return noop
}

// Unless is the opposite of [If]: it writes stmt only if cond is false. A
// template cannot negate a condition (${If(!signedIn, `x`)} is a compile
// error), so it writes ${Unless(signedIn, `x`)} instead.
func Unless(cond bool, stmt any) TagFunc {
	if !cond {
		return value(stmt)
	}
	return noop
}

// noop is a [TagFunc] which writes nothing. It is shared, so returning it
// costs no allocation.
var noop TagFunc = func(io.Writer, string) (int, error) { return 0, nil }

// value returns a [TagFunc] which writes v with [writeValue]. For an empty v
// (nil, "" or an empty []byte) it returns [noop], so nothing is allocated and
// nothing is written. If v already is a [TagFunc], it is returned as it is.
func value(v any) TagFunc {
	switch t := v.(type) {
	case nil:
		return noop
	case TagFunc:
		return t
	case string:
		if t == `` {
			return noop
		}
	case []byte:
		if len(t) == 0 {
			return noop
		}
	}
	return func(w io.Writer, tag string) (int, error) {
		return writeValue(w, tag, v)
	}
}
