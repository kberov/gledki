package gledki

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"strconv"
	"strings"
)

// argKind is the kind of an [argument] of a [call].
type argKind int

const (
	argKey     argKind = iota // a stash key: text is the key
	argLiteral                // a string literal: text is its value
	argCall                   // a nested call: call is set
)

// argument is one argument of a [call], as it is written in the template.
type argument struct {
	kind argKind
	text string
	call *call
}

// call is a ready call of a [builtin], parsed from a tag content, for
// example ${IfElse(u, `/a.svg`, `/b.svg`)}.
type call struct {
	text string // the tag content, for messages
	name string
	fn   *builtin
	args []argument
}

// builtin is a function, which can be called from a template.
type builtin struct {
	arity int
	run   func(e *execution, w io.Writer, c *call, args []any) (int, error)
}

// execution is the state of one [Gledki.Exec], passed to the builtins.
type execution struct {
	t     *Gledki
	stash Stash
	depth int // how many Exec calls deep
}

// builtins are the functions, which can be called from a template, by name.
var builtins map[string]*builtin

func init() {
	builtins = map[string]*builtin{
		"If":     fn2(If),
		"IfElse": fn3(IfElse),
		"Exec":   {arity: 1, run: runExec},
		"Set":    StashFunc2(set).b,
	}
}

// set is the builtin Set: ${Set(`key`, value)} writes `v` under `key` in the
// stash `s` and writes nothing. The tags after it and the templates executed
// with Exec after it see the value. It panics, when the stash is nil.
func set(s Stash, key string, v any) TagFunc {
	if s == nil {
		panic(fmt.Sprintf("Set(%q): the stash is nil", key))
	}
	s[key] = v
	return noop
}

// run runs the call `c` in the execution `e` and writes its output to `w`.
// The arguments get their values first: a stash key the value from the stash
// (nil, when the stash has no such key), a literal its text, and a nested
// call a [TagFunc], which runs the nested call only when it is written.
func (c *call) run(e *execution, w io.Writer) (int, error) {
	args := make([]any, len(c.args))
	for i, a := range c.args {
		switch a.kind {
		case argKey:
			args[i] = keyValue(e.stash, a.text)
		case argLiteral:
			args[i] = a.text
		case argCall:
			args[i] = a.call.tagFunc(e)
		}
	}
	return c.fn.run(e, w, c, args)
}

// keyValue returns the value of `key` in `stash`. A [TagFunc] is wrapped, so
// that it is called with its own key as the tag, as when it is the value of a
// placeholder, and not with the text of the call it is passed to.
func keyValue(stash Stash, key string) any {
	v := stash[key]
	if tf, ok := v.(TagFunc); ok {
		return TagFunc(func(w io.Writer, _ string) (int, error) { return tf(w, key) })
	}
	return v
}

// tagFunc returns a [TagFunc], which runs the call `c` in the execution `e`.
func (c *call) tagFunc(e *execution) TagFunc {
	return func(w io.Writer, _ string) (int, error) {
		return c.run(e, w)
	}
}

// arg returns the argument `i` of the call `c` from its values `args` as a
// `T`. A nil value is returned as the zero `T`, when `T` is an interface type
// (`any`), so a missing stash key reaches such a parameter as nil. Any other
// value, which is not a `T`, panics with the call and the argument: a missing
// bool silently taken as false would hide a typo in the template.
func arg[T any](c *call, args []any, i int) T {
	v, ok := args[i].(T)
	if !ok && (args[i] != nil || any(v) != nil) {
		panic(fmt.Sprintf("%s: argument %d (%s) is %T, not %T", c.text, i+1, c.args[i].text, args[i], v))
	}
	return v
}

// fn1 makes a builtin from the Go function `f` with one parameter. The
// result of `f` is written like a stash value (nil, string, []byte or
// [TagFunc]).
func fn1[A, R any](f func(A) R) *builtin {
	return &builtin{arity: 1, run: func(_ *execution, w io.Writer, c *call, args []any) (int, error) {
		return writeValue(w, c.text, f(arg[A](c, args, 0)))
	}}
}

// fn2 makes a builtin from the Go function `f` with two parameters, like
// [fn1].
func fn2[A, B, R any](f func(A, B) R) *builtin {
	return &builtin{arity: 2, run: func(_ *execution, w io.Writer, c *call, args []any) (int, error) {
		return writeValue(w, c.text, f(arg[A](c, args, 0), arg[B](c, args, 1)))
	}}
}

// fn3 makes a builtin from the Go function `f` with three parameters, like
// [fn1].
func fn3[A, B, C, R any](f func(A, B, C) R) *builtin {
	return &builtin{arity: 3, run: func(_ *execution, w io.Writer, c *call, args []any) (int, error) {
		return writeValue(w, c.text, f(arg[A](c, args, 0), arg[B](c, args, 1), arg[C](c, args, 2)))
	}}
}

// errUnknownFunc is wrapped by the error of [parseCall] for a call of a
// function, which is not a builtin.
var errUnknownFunc = errors.New("unknown function")

// parseCall parses the tag content `content` with [parser.ParseExpr]. It
// returns:
//   - nil, nil, when `content` is not a call (for example `title` or `a b`);
//   - nil and an error wrapping [errUnknownFunc] for a call of a function,
//     which is not a builtin;
//   - nil and an error for a wrong call of a builtin, also for an
//     expression, which is not a call, but contains one (see [builtinInside]);
//   - the ready call otherwise.
func parseCall(content string) (*call, error) {
	if !builtinsRead.Load() {
		builtinsRead.Store(true)
	}
	expr, err := parser.ParseExpr(content)
	if err != nil {
		if startsWithBuiltin(content) {
			return nil, fmt.Errorf("cannot parse %q: %v", content, err)
		}
		return nil, nil
	}
	ce, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, builtinInside(content, expr)
	}
	return parseCallExpr(content, ce)
}

// builtinInside returns an error, when the expression `expr`, which is not a
// call, contains a call of a builtin, for example (If(a, `x`)) or
// If(a, `x`).Y: it would stay a placeholder, which writes nothing. Other
// expressions (also with calls of other functions, as in JavaScript) are not
// for gledki, so they are no error.
func builtinInside(content string, expr ast.Expr) error {
	var err error
	ast.Inspect(expr, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok || err != nil {
			return err == nil
		}
		if ident, ok := ce.Fun.(*ast.Ident); ok && builtins[ident.Name] != nil {
			err = fmt.Errorf("%s: a call of a builtin must be the whole tag content", source(content, expr))
		}
		return err == nil
	})
	return err
}

// startsWithBuiltin tells whether `content` starts with the name of a builtin
// followed by "(".
func startsWithBuiltin(content string) bool {
	name, _, found := strings.Cut(content, "(")
	_, known := builtins[strings.TrimSpace(name)]
	return found && known
}

// parseCallExpr makes a call from the call expression `ce` in `content`.
func parseCallExpr(content string, ce *ast.CallExpr) (*call, error) {
	ident, ok := ce.Fun.(*ast.Ident)
	if !ok || builtins[ident.Name] == nil {
		return nil, fmt.Errorf("%w %s", errUnknownFunc, source(content, ce.Fun))
	}
	fn := builtins[ident.Name]
	if ce.Ellipsis.IsValid() {
		return nil, notAllowed(content, ce)
	}
	if len(ce.Args) != fn.arity {
		return nil, fmt.Errorf("%s takes %d arguments, got %d", ident.Name, fn.arity, len(ce.Args))
	}
	c := &call{text: source(content, ce), name: ident.Name, fn: fn, args: make([]argument, len(ce.Args))}
	for i, a := range ce.Args {
		arg, err := parseArgument(content, a)
		if err != nil {
			return nil, err
		}
		c.args[i] = arg
	}
	return c, nil
}

// parseArgument makes an argument from the expression `expr` in `content`.
func parseArgument(content string, expr ast.Expr) (argument, error) {
	switch e := expr.(type) {
	case *ast.Ident:
		return argument{kind: argKey, text: e.Name}, nil
	case *ast.BasicLit:
		return literalArgument(content, e)
	case *ast.CallExpr:
		return callArgument(content, e)
	case *ast.SelectorExpr:
		return argument{}, fmt.Errorf("dot notation is not supported yet: %s", source(content, e))
	default:
		return argument{}, notAllowed(content, e)
	}
}

// literalArgument makes an argument from a string literal; other literals
// (numbers, characters) are not allowed.
func literalArgument(content string, lit *ast.BasicLit) (argument, error) {
	if lit.Kind != token.STRING {
		return argument{}, fmt.Errorf("only string literals are allowed, got %s", source(content, lit))
	}
	text, err := strconv.Unquote(lit.Value)
	if err != nil {
		return argument{}, fmt.Errorf("cannot unquote %s: %v", lit.Value, err)
	}
	return argument{kind: argLiteral, text: text}, nil
}

// callArgument makes an argument from a nested call of a builtin.
func callArgument(content string, ce *ast.CallExpr) (argument, error) {
	c, err := parseCallExpr(content, ce)
	if err != nil {
		return argument{}, err
	}
	return argument{kind: argCall, call: c}, nil
}

// notAllowed returns the error for an expression, which is not allowed in a
// call.
func notAllowed(content string, n ast.Node) error {
	return fmt.Errorf("%s is not allowed in a call; only stash keys, string literals and calls are",
		source(content, n))
}

// source returns the text of the node `n` in `content`, as it is written.
func source(content string, n ast.Node) string {
	return content[n.Pos()-1 : n.End()-1]
}

// runExec runs [Gledki.Exec] called from a template: ${Exec(`path`)} or
// ${Exec(key)}. The template file is executed with the same stash, one Exec
// call deeper. The path must be a string; nested Exec calls are limited by
// [Gledki.IncludeLimit], so a cycle panics instead of never ending.
func runExec(e *execution, w io.Writer, c *call, args []any) (int, error) {
	path := arg[string](c, args, 0)
	if e.depth+1 > e.t.IncludeLimit {
		e.t.Logger.Panicf("Limit of %d nested Exec calls reached while trying to execute %s",
			e.t.IncludeLimit, path)
	}
	n, err := e.t.exec(w, path, e.stash, e.depth+1)
	return int(n), err
}
