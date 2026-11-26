package gledki

import (
	"errors"
	"os"
	"strings"
	"sync"
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

// callCase is one case of [TestCalls]: the template files to write (the main
// one is always `page.htm`), the stash to execute `page` with, and the
// expected output or a part of the expected panic.
type callCase struct {
	name   string
	files  map[string]string
	stash  Stash
	want   string
	panics string
}

// page returns the files of a case with only the main template text `text`.
func page(text string) map[string]string { return map[string]string{"page.htm": text} }

// callCases are the cases of [TestCalls].
var callCases = []callCase{
	{name: "ifelse true", files: page("<img src=\"${IfElse(u, `/a.svg`, `/b.svg`)}\">"),
		stash: Stash{"u": true}, want: `<img src="/a.svg">`},
	{name: "ifelse false", files: page("<img src=\"${IfElse(u, `/a.svg`, `/b.svg`)}\">"),
		stash: Stash{"u": false}, want: `<img src="/b.svg">`},
	{name: "if with key", files: page("[${If(u, name)}]"),
		stash: Stash{"u": true, "name": "N"}, want: "[N]"},
	{name: "if false", files: page("[${If(u, name)}]"),
		stash: Stash{"u": false, "name": "N"}, want: "[]"},
	{name: "missing value", files: page("[${If(u, name)}]"),
		stash: Stash{"u": true}, want: "[]"},
	{name: "stash wins", files: page("${If(u, `x`)}"),
		stash: Stash{"If(u, `x`)": "S", "u": true}, want: "S"},
	{name: "stash nil wins", files: page("${If(u, `x`)}"),
		stash: Stash{"If(u, `x`)": nil, "u": true}, want: ""},
	{name: "spaces trimmed", files: page("${ If(u, `x`) }"),
		stash: Stash{"u": true}, want: "x"},
	{name: "missing condition", files: page("${If(u, `x`)}"),
		stash: Stash{}, panics: "If(u, `x`): condition u is <nil>, not bool"},
	{name: "string condition", files: page("${If(u, `x`)}"),
		stash: Stash{"u": "yes"}, panics: "condition u is string, not bool"},
}

// TestCalls checks the calls of builtins, executed by [Gledki.Exec].
func TestCalls(t *testing.T) {
	for _, c := range callCases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, c.files)
			tpls := newIn(t, root, tagsPair)
			var out strings.Builder
			msg := panicMessage(func() {
				if _, err := tpls.Exec(&out, "page", c.stash); err != nil {
					t.Fatal(err)
				}
			})
			if c.panics != "" {
				if !strings.Contains(msg, c.panics) {
					t.Fatalf("the panic must contain %q, got %q", c.panics, msg)
				}
				return
			}
			if msg != "" {
				t.Fatalf("unexpected panic: %s", msg)
			}
			if out.String() != c.want {
				t.Fatalf("got %q, want %q", out.String(), c.want)
			}
		})
	}
}

// TestCallsFromCompiledFile checks that the calls work also when the compiled
// template is read from the compiled file, and that a compiled file with a
// wrong call is not used.
func TestCallsFromCompiledFile(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, page("${If(u, `x`)}"))
	exec := func() string {
		var out strings.Builder
		if _, err := newIn(t, root, tagsPair).Exec(&out, "page", Stash{"u": true}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := exec(); got != "x" {
		t.Fatalf("first: got %q", got)
	}
	if got := exec(); got != "x" {
		t.Fatalf("from the compiled file: got %q", got)
	}
	compiledFile := newIn(t, root, tagsPair).toFullPath("page") + CompiledSuffix
	if err := os.WriteFile(compiledFile, []byte("${If(u)}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := exec(); got != "x" {
		t.Fatalf("from a wrong compiled file: got %q", got)
	}
}

// TestCallsConcurrent checks that many goroutines can execute the same
// template with calls at the same time, each with its own stash.
func TestCallsConcurrent(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, page("${IfElse(u, `T`, `F`)}"))
	tpls := newIn(t, root, tagsPair)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			want := map[bool]string{true: "T", false: "F"}[i%2 == 0]
			var out strings.Builder
			if _, err := tpls.Exec(&out, "page", Stash{"u": i%2 == 0}); err != nil {
				t.Error(err)
			}
			if out.String() != want {
				t.Errorf("goroutine %d: got %q, want %q", i, out.String(), want)
			}
		})
	}
	wg.Wait()
}
