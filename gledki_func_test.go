package gledki

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/labstack/gommon/log"
)

// TestMain registers the functions of the tests after every init() of the
// package and before any test compiles a template.
func TestMain(m *testing.M) {
	registerTestFuncs()
	os.Exit(m.Run())
}

func registerTestFuncs() {
	Func("Pick", StashFunc2(pick))
	Func("Hello", StashFunc0(hello))
	Func("Upper", StashFunc1(upper))
	Func("Mark", StashFunc1(mark))
	Func("Fail", StashFunc0(fail))
}

// errFail is the panic value of [fail].
var errFail = errors.New("failed on purpose")

// fail panics with [errFail].
func fail(Stash) string { panic(errFail) }

// pick returns "selected", when the value of `key` in the stash is `want`.
func pick(s Stash, key, want string) string {
	if fmt.Sprint(s[key]) == want {
		return "selected"
	}
	return ""
}

func hello(s Stash) string { return "Hello, " + fmt.Sprint(s["name"]) }

func upper(_ Stash, v string) []byte { return []byte(strings.ToUpper(v)) }

// mark writes true under `key` in the stash and returns [writeTag].
func mark(s Stash, key string) TagFunc {
	s[key] = true
	return writeTag
}

// funcCases are the cases of [TestFuncCalls].
var funcCases = []callCase{
	{name: "pick selected", files: page("<option ${Pick(`t`, `1`)}>"),
		stash: Stash{"t": "1"}, want: "<option selected>"},
	{name: "pick not selected", files: page("<option ${Pick(`t`, `1`)}>"),
		stash: Stash{"t": "2"}, want: "<option >"},
	{name: "pick missing key", files: page("[${Pick(`t`, `1`)}]"),
		stash: Stash{}, want: "[]"},
	{name: "no arguments", files: page("${Hello()}"),
		stash: Stash{"name": "Ana"}, want: "Hello, Ana"},
	{name: "bytes result", files: page("${Upper(`ab`)}"), stash: Stash{}, want: "AB"},
	{name: "stash key argument", files: page("${Upper(v)}"), stash: Stash{"v": "x"}, want: "X"},
	{name: "tagfunc result and write", files: page("${Mark(`seen`)}${If(seen, `!`)}"),
		stash: Stash{}, want: "Mark(`seen`)!"},
	{name: "inside If", files: page("[${If(u, Pick(`t`, `1`))}]"),
		stash: Stash{"u": true, "t": "1"}, want: "[selected]"},
	{name: "wrong argument type", files: page("${Upper(v)}"),
		stash: Stash{"v": 5}, panics: "template \"page\": Upper(v): argument 1 (v) is int, not string"},
	{name: "set literal", files: page("${Set(`t`, `T`)}[${t}]"), stash: Stash{}, want: "[T]"},
	{name: "set from key", files: page("${Set(`t`, name)}[${t}]"),
		stash: Stash{"name": "N"}, want: "[N]"},
	{name: "set before and after", files: page("[${t}]${Set(`t`, `T`)}[${t}]"),
		stash: Stash{}, want: "[][T]"},
	{name: "set not written", files: page("${If(u, Set(`t`, `T`))}[${t}]"),
		stash: Stash{"u": false}, want: "[]"},
	{name: "set in exec", files: map[string]string{
		"page.htm": "[${t}]${Exec(`p`)}[${t}]", "p.htm": "${Set(`t`, `T`)}"},
		stash: Stash{}, want: "[][T]"},
	{name: "set bool for if", files: page("${Set(`u`, flag)}${If(u, `x`)}"),
		stash: Stash{"flag": true}, want: "x"},
	{name: "set exec cycle", files: map[string]string{
		"page.htm": "${Set(`x`, Exec(`p`))}${x}", "p.htm": "${x}"},
		stash: Stash{}, panics: "nested Exec calls reached while trying to execute p"},
	{name: "set exec written twice", files: map[string]string{
		"page.htm": "${Set(`x`, Exec(`p`))}[${x}${x}]", "p.htm": "P"},
		stash: Stash{}, want: "[PP]"},
	{name: "set key missing", files: page("${Set(k, `T`)}"),
		stash: Stash{}, panics: "argument 1 (k) is <nil>, not string"},
	{name: "set nested with args", files: page("${Set(`x`, IfElse(u, Pick(`t`, `1`), `no`))}[${x}|${Pick(`t`, `2`)}|${x}]"),
		stash: Stash{"u": true, "t": "1"}, want: "[selected||selected]"},
	{name: "set key not string", files: page("${Set(k, `T`)}"),
		stash: Stash{"k": 5}, panics: "argument 1 (k) is int, not string"},
}

// TestFuncCalls checks the calls of the functions of the user, registered
// with [Func].
func TestFuncCalls(t *testing.T) { runCallCases(t, funcCases) }

// TestFuncCompileErrors checks that a wrong call of a function of the user is
// a compile error with the template file and the line.
func TestFuncCompileErrors(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, page("${Pick(`t`)}"))
	_, err := newIn(t, root, tagsPair).Compile("page")
	const want = "page.htm, line 1: Pick takes 2 arguments, got 1"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("the error must contain %q, got %v", want, err)
	}
}

// funcPanicCase is one case of [TestFuncPanics]: the name and the function
// given to [Func] and a part of the expected panic.
type funcPanicCase struct {
	name   string
	fn     Function
	panics string
}

// funcPanicCases are the cases of [TestFuncPanics].
var funcPanicCases = []funcPanicCase{
	{name: "Nothing", fn: Function{}, panics: `gledki.Func("Nothing"): no function`},
	{name: "a b", fn: StashFunc0(hello), panics: `gledki.Func("a b"): not a Go identifier`},
	{name: "If", fn: StashFunc0(hello), panics: `gledki.Func("If"): already registered`},
	{name: "Pick", fn: StashFunc0(hello), panics: `gledki.Func("Pick"): already registered`},
	{name: "Set", fn: StashFunc0(hello), panics: `gledki.Func("Set"): already registered`},
	{name: "Unless", fn: StashFunc0(hello), panics: `gledki.Func("Unless"): already registered`},
	{name: "Exec", fn: StashFunc0(hello), panics: `gledki.Func("Exec"): already registered`},
}

// TestFuncPanics checks that [Func] refuses a wrong registration.
func TestFuncPanics(t *testing.T) {
	for _, c := range funcPanicCases {
		t.Run(c.name, func(t *testing.T) {
			msg := panicMessage(func() { Func(c.name, c.fn) })
			if !strings.Contains(msg, c.panics) {
				t.Fatalf("the panic must contain %q, got %q", c.panics, msg)
			}
		})
	}
}

// TestFuncAfterCompile checks that [Func] panics after the first compile and
// does not register the function.
func TestFuncAfterCompile(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "${If(u, `x`)}", "late.htm": "${Late()}"})
	tpls := newIn(t, root, tagsPair)
	if _, err := tpls.Compile("page"); err != nil {
		t.Fatal(err)
	}
	const want = "templates are already compiled; register functions in init()"
	if msg := panicMessage(func() { Func("Late", StashFunc0(hello)) }); !strings.Contains(msg, want) {
		t.Fatalf("the panic must contain %q, got %q", want, msg)
	}
	var logged strings.Builder
	tpls.Logger = log.New("test")
	tpls.Logger.SetOutput(&logged)
	tpls.Logger.SetLevel(log.WARN)
	if _, err := tpls.Compile("late"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logged.String(), "unknown function Late") {
		t.Fatalf("Late must not be registered; the warning is %q", logged.String())
	}
}

// TestSetNilStash checks that Set panics with the template and the key, when
// the stash is nil.
func TestSetNilStash(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, page("${Set(`t`, `T`)}"))
	tpls := newIn(t, root, tagsPair)
	var out strings.Builder
	msg := panicMessage(func() { _, _ = tpls.Exec(&out, "page", nil) })
	const want = `template "page": Set("t"): the stash is nil`
	if !strings.Contains(msg, want) {
		t.Fatalf("the panic must contain %q, got %q", want, msg)
	}
}

// BenchmarkCallKinds compares a call of a builtin (IfElse) with a call of a
// function of the user (Pick).
func BenchmarkCallKinds(b *testing.B) {
	kinds := []struct{ name, tag string }{
		{"IfElse", "${IfElse(u, `a`, `b`)}"},
		{"Pick", "${Pick(`t`, `1`)}"},
	}
	stash := Stash{"u": true, "t": "1"}
	for _, k := range kinds {
		b.Run(k.name, func(b *testing.B) {
			root := b.TempDir()
			writeFiles(b, root, page(strings.Repeat(k.tag, 20)))
			tpls := newIn(b, root, tagsPair)
			if _, err := tpls.Exec(io.Discard, "page", stash); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := tpls.Exec(io.Discard, "page", stash); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestFuncAfterCompileWithoutTags checks that [Func] panics also after the
// compile of a template without any tag. builtinsRead is cleared for the
// test, as at the start of a program, and set again at its end.
func TestFuncAfterCompileWithoutTags(t *testing.T) {
	builtinsRead.Store(false)
	t.Cleanup(func() { builtinsRead.Store(true) })
	root := t.TempDir()
	writeFiles(t, root, page("<p>no tags</p>"))
	if _, err := newIn(t, root, tagsPair).Compile("page"); err != nil {
		t.Fatal(err)
	}
	const want = "templates are already compiled"
	if msg := panicMessage(func() { Func("Plain", StashFunc0(hello)) }); !strings.Contains(msg, want) {
		t.Fatalf("the panic must contain %q, got %q", want, msg)
	}
}

// TestPanicErrorKept checks that a panic with an error keeps the error, so
// errors.Is finds it, and gets the chain of templates in front of its message.
func TestPanicErrorKept(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "${Exec(`p`)}", "p.htm": "${Fail()}"})
	tpls := newIn(t, root, tagsPair)
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = tpls.Exec(io.Discard, "page", Stash{})
	}()
	err, ok := recovered.(error)
	if !ok || !errors.Is(err, errFail) {
		t.Fatalf("the panic must be an error wrapping errFail, got %#v", recovered)
	}
	const want = `template "page": template "p": failed on purpose`
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

// TestFuncAllocs checks that a call of a function of the user, which returns
// a string, allocates only the slice for the arguments of the execution (once
// for all its calls, see [execution.args]), as a call of If does.
func TestFuncAllocs(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"plain.htm": "<p>x</p>",
		"pick.htm":  "<p ${Pick(`t`, `1`)}>",
		"upper.htm": "<p>${Upper(`ab`)}</p>",
		"hello.htm": "<p>${Hello()}</p>",
	})
	tpls := newIn(t, root, tagsPair)
	stash := Stash{"t": "1", "name": "Ana"}
	allocs := func(path string) float64 {
		if _, err := tpls.Exec(io.Discard, path, stash); err != nil {
			t.Fatal(err)
		}
		return testing.AllocsPerRun(100, func() { _, _ = tpls.Exec(io.Discard, path, stash) })
	}
	plain := allocs("plain")
	for _, c := range []struct {
		path string
		want float64
	}{
		{"pick", 1},  // the slice for the arguments
		{"upper", 3}, // the slice, and the string and the []byte of upper
		{"hello", 2}, // the two strings of hello (no arguments: no slice)
	} {
		if got := allocs(c.path) - plain; got != c.want {
			t.Errorf("%s allocates %v times more than a page without calls, want %v", c.path, got, c.want)
		}
	}
}
