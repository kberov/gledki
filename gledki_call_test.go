package gledki

import (
	"errors"
	"io"
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
	{"(If(a, `x`))", "", "(If(a, `x`)): a call of a builtin must be the whole tag content"},
	{"If(a, `x`).Y", "", "a call of a builtin must be the whole tag content"},
	{"Math.max(a, b) + 1", "", ""},
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
	{"builtin inside", "\n${If(a, `x`).Y}", "page.htm, line 2: If(a, `x`).Y: a call of a builtin must be the whole tag content"},
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
		stash: Stash{}, panics: "If(u, `x`): argument 1 (u) is <nil>, not bool"},
	{name: "string condition", files: page("${If(u, `x`)}"),
		stash: Stash{"u": "yes"}, panics: "argument 1 (u) is string, not bool"},
	{name: "exec literal", files: map[string]string{"page.htm": "[${Exec(`p`)}]", "p.htm": "${title}"},
		stash: Stash{"title": "T"}, want: "[T]"},
	{name: "exec key", files: map[string]string{"page.htm": "[${Exec(form)}]", "p.htm": "P"},
		stash: Stash{"form": "p"}, want: "[P]"},
	{name: "exec skipped", files: page("[${If(u, Exec(`missing`))}]"),
		stash: Stash{"u": false}, want: "[]"},
	{name: "exec in if", files: map[string]string{"page.htm": "[${If(u, Exec(`p`))}]", "p.htm": "P"},
		stash: Stash{"u": true}, want: "[P]"},
	{name: "path not string", files: page("${Exec(form)}"),
		stash: Stash{"form": 5}, panics: "Exec(form): argument 1 (form) is int, not string"},
	{name: "exec cycle", files: page("${Exec(`page`)}"),
		stash: Stash{}, panics: "nested Exec calls reached while trying to execute page"},
	{name: "panic names the template", files: page("${If(u, `x`)}"),
		stash: Stash{}, panics: "template \"page\": If(u, `x`): argument 1 (u) is <nil>, not bool"},
	{name: "panic names the Exec chain", files: map[string]string{"page.htm": "${Exec(`p`)}", "p.htm": "${If(u, `x`)}"},
		stash: Stash{}, panics: "template \"page\": template \"p\": If(u, `x`): argument 1 (u)"},
	{name: "call before unclosed tag", files: page("[${If(u, `x`)}] ${"),
		stash: Stash{"u": true}, want: "[x] ${",
	},
	{name: "tagfunc gets its key", files: page("[${If(u, f)}|${IfElse(u, g, `b`)}]"),
		stash: Stash{"u": true, "f": writeTag, "g": writeTag}, want: "[f|g]"},
}

// writeTag is a [TagFunc], which writes the tag it is called with.
var writeTag TagFunc = func(w io.Writer, tag string) (int, error) { return io.WriteString(w, tag) }

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

// TestExecMissingFile checks that Exec called from a template returns the
// error of a template file, which cannot be read.
func TestExecMissingFile(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, page("${Exec(`missing`)}"))
	var out strings.Builder
	_, err := newIn(t, root, tagsPair).Exec(&out, "page", Stash{})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("the error must contain %q, got %v", "missing", err)
	}
}

// argCase is one case of [TestArg]: the value of the only argument `a` of the
// call F(a), the function, which takes it with [arg] of some type, and the
// expected value or a part of the expected panic.
type argCase struct {
	name   string
	value  any
	take   func(c *call, args []any) any
	want   any
	panics string
}

func takeBool(c *call, args []any) any   { return arg[bool](c, args, 0) }
func takeAny(c *call, args []any) any    { return arg[any](c, args, 0) }
func takeString(c *call, args []any) any { return arg[string](c, args, 0) }

// argCases are the cases of [TestArg].
var argCases = []argCase{
	{name: "bool", value: true, take: takeBool, want: true},
	{name: "nil to any", value: nil, take: takeAny, want: nil},
	{name: "nil to bool", value: nil, take: takeBool, panics: "F(a): argument 1 (a) is <nil>, not bool"},
	{name: "string to bool", value: "yes", take: takeBool, panics: "argument 1 (a) is string, not bool"},
	{name: "int to string", value: 5, take: takeString, panics: "argument 1 (a) is int, not string"},
}

// TestArg checks that [arg] returns an argument of the right type, passes nil
// to an interface type and panics with one message for a wrong type.
func TestArg(t *testing.T) {
	c := &call{text: "F(a)", args: []argument{{kind: argKey, text: "a"}}}
	for _, ac := range argCases {
		t.Run(ac.name, func(t *testing.T) {
			var got any
			msg := panicMessage(func() { got = ac.take(c, []any{ac.value}) })
			if ac.panics != "" {
				if !strings.Contains(msg, ac.panics) {
					t.Fatalf("the panic must contain %q, got %q", ac.panics, msg)
				}
				return
			}
			if msg != "" {
				t.Fatalf("unexpected panic: %s", msg)
			}
			if got != ac.want {
				t.Fatalf("got %#v, want %#v", got, ac.want)
			}
		})
	}
}

// TestFn1 checks that [fn1] makes a builtin from a Go function with one typed
// parameter and writes its result.
func TestFn1(t *testing.T) {
	b := fn1(strings.ToUpper)
	c := &call{text: "U(a)", fn: b, args: []argument{{kind: argKey, text: "a"}}}
	var out strings.Builder
	if _, err := c.run(&execution{stash: Stash{"a": "x"}}, &out); err != nil {
		t.Fatal(err)
	}
	if b.arity != 1 || out.String() != "X" {
		t.Fatalf("arity %d, output %q; want 1, %q", b.arity, out.String(), "X")
	}
}
