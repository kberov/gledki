package gledki

import (
	"errors"
	"strings"
	"testing"
)

// describe prints the call `c` for the tests: If(key:a, lit:x), nested calls
// in the same way; "" for nil.
func describe(c *call) string {
	if c == nil {
		return ""
	}
	parts := make([]string, len(c.args))
	for i, a := range c.args {
		switch a.kind {
		case argKey:
			parts[i] = "key:" + a.text
		case argLiteral:
			parts[i] = "lit:" + a.text
		case argCall:
			parts[i] = describe(a.call)
		}
	}
	return c.name + "(" + strings.Join(parts, ", ") + ")"
}

// parseCallCase is one case of [TestParseCall]: a tag content, the expected
// call (as [describe] prints it) and a part of the expected error ("unknown"
// means an error wrapping [errUnknownFunc]; "" means no error).
type parseCallCase struct {
	content string
	want    string
	err     string
}

// parseCallCases are the cases of [TestParseCall].
var parseCallCases = []parseCallCase{
	{"title", "", ""},
	{"a b", "", ""},
	{"inv.Total", "", ""},
	{"If(a, `x`)", "If(key:a, lit:x)", ""},
	{"IfElse(a, \"x\", Exec(`p`))", "IfElse(key:a, lit:x, Exec(lit:p))", ""},
	{"Exec(form)", "Exec(key:form)", ""},
	{"Iff(a, b)", "", "unknown"},
	{"lang(en)", "", "unknown"},
	{"fmt.Sprintf(x)", "", "unknown"},
	{"If(a)", "", "If takes 2 arguments, got 1"},
	{"If(!a, b)", "", "!a is not allowed in a call"},
	{"If(a, 1)", "", "only string literals are allowed, got 1"},
	{"If(a, inv.Total)", "", "dot notation is not supported yet: inv.Total"},
	{"IfElse(a, `", "", "cannot parse"},
}

// TestParseCall checks which tag contents are calls of builtins and how they
// are parsed.
func TestParseCall(t *testing.T) {
	for _, c := range parseCallCases {
		t.Run(c.content, func(t *testing.T) {
			got, err := parseCall(c.content)
			switch {
			case c.err == "unknown":
				if !errors.Is(err, errUnknownFunc) {
					t.Fatalf("want an unknown function, got %v", err)
				}
			case c.err != "":
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("the error must contain %q, got %v", c.err, err)
				}
			case err != nil:
				t.Fatalf("unexpected error: %v", err)
			}
			if describe(got) != c.want {
				t.Fatalf("got %q, want %q", describe(got), c.want)
			}
		})
	}
}
