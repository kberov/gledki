package gledki

import (
	"errors"
	"strings"
	"testing"

	"github.com/labstack/gommon/log"
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

// compileCallErrorCases are the cases of [TestCompileCallErrors]: the main
// template text and what the error of compiling it must contain.
var compileCallErrorCases = []struct{ name, page, want string }{
	{"wrong arity", "a\n${If(a)}", "page.htm, line 2: If takes 2 arguments, got 1"},
	{"operator", "${If(!a, b)}", "page.htm, line 1: !a is not allowed in a call"},
	{"end tag in literal", "${IfElse(a, `}`, `x`)}", "page.htm, line 1: cannot parse"},
}

// TestCompileCallErrors checks that a wrong call of a builtin is a compile
// error with the template file and the line.
func TestCompileCallErrors(t *testing.T) {
	for _, c := range compileCallErrorCases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, map[string]string{"page.htm": c.page})
			_, err := newIn(t, root, tagsPair).Compile("page")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the error must contain %q, got %v", c.want, err)
			}
		})
	}
}

// TestUnknownFunctionWarning checks that a call of an unknown function stays
// a placeholder and is logged as a warning, and that MustCompile panics with
// it.
func TestUnknownFunctionWarning(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "a\n${Iff(a, b)}"})
	tpls := newIn(t, root, tagsPair)
	var logged strings.Builder
	tpls.Logger = log.New("test")
	tpls.Logger.SetOutput(&logged)
	tpls.Logger.SetLevel(log.WARN)

	text, err := tpls.Compile("page")
	if err != nil || text != "a\n${Iff(a, b)}" {
		t.Fatalf("Compile: got %q, %v", text, err)
	}
	const want = "page.htm, line 2: unknown function Iff in ${Iff(a, b)}, written as a placeholder"
	if !strings.Contains(logged.String(), want) {
		t.Errorf("the warning must contain %q, got %q", want, logged.String())
	}
	if msg := panicMessage(func() { tpls.MustCompile("page") }); !strings.Contains(msg, want) {
		t.Errorf("MustCompile must panic with %q, got %q", want, msg)
	}
}
