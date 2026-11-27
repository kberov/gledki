package gledki

import (
	"strings"
	"testing"
)

// ifCase is one case of [TestIf]: a TagFunc, returned by [If] or [IfElse],
// and what it writes.
type ifCase struct {
	name string
	f    TagFunc
	want string
}

// ifCases are the cases of [TestIf].
var ifCases = []ifCase{
	{"if true string", If(true, "x"), "x"},
	{"if false", If(false, "x"), ""},
	{"if true bytes", If(true, []byte("b")), "b"},
	{"if true TagFunc", If(true, IfElse(false, "a", "e")), "e"},
	{"if true nil", If(true, nil), ""},
	{"ifelse true", IfElse(true, "a", "e"), "a"},
	{"ifelse false empty", IfElse(false, "a", ""), ""},
	{"unless false", Unless(false, "x"), "x"},
	{"unless true", Unless(true, "x"), ""},
	{"unless false nil", Unless(false, nil), ""},
}

// TestIf checks what the TagFuncs, returned by [If] and [IfElse], write.
func TestIf(t *testing.T) {
	for _, c := range ifCases {
		t.Run(c.name, func(t *testing.T) {
			var out strings.Builder
			if _, err := c.f(&out, "t"); err != nil {
				t.Fatal(err)
			}
			if out.String() != c.want {
				t.Fatalf("got %q, want %q", out.String(), c.want)
			}
		})
	}
}
