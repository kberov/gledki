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
		"If":     {arity: 2, run: runIf},
		"IfElse": {arity: 3, run: runIfElse},
		"Exec":   {arity: 1, run: runExec},
	}
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
			args[i] = e.stash[a.text]
		case argLiteral:
			args[i] = a.text
		case argCall:
			args[i] = a.call.tagFunc(e)
		}
	}
	return c.fn.run(e, w, c, args)
}

// tagFunc returns a [TagFunc], which runs the call `c` in the execution `e`.
func (c *call) tagFunc(e *execution) TagFunc {
	return func(w io.Writer, _ string) (int, error) {
		return c.run(e, w)
	}
}

// condition returns the first argument of the call `c` as a bool. It panics,
// when the value is not a bool (also when the stash has no such key): a
// missing value silently taken as false would hide a typo in the template.
func (c *call) condition(args []any) bool {
	cond, ok := args[0].(bool)
	if !ok {
		panic(fmt.Sprintf("%s: condition %s is %T, not bool", c.text, c.args[0].text, args[0]))
	}
	return cond
}

// runIf runs [If] called from a template: ${If(cond, stmt)}.
func runIf(_ *execution, w io.Writer, c *call, args []any) (int, error) {
	return If(c.condition(args), args[1])(w, c.text)
}

// runIfElse runs [IfElse] called from a template: ${IfElse(cond, then, els)}.
func runIfElse(_ *execution, w io.Writer, c *call, args []any) (int, error) {
	return IfElse(c.condition(args), args[1], args[2])(w, c.text)
}

// errUnknownFunc is wrapped by the error of [parseCall] for a call of a
// function, which is not a builtin.
var errUnknownFunc = errors.New("unknown function")

// parseCall parses the tag content `content` with [parser.ParseExpr]. It
// returns:
//   - nil, nil, when `content` is not a call (for example `title` or `a b`);
//   - nil and an error wrapping [errUnknownFunc] for a call of a function,
//     which is not a builtin;
//   - nil and an error for a wrong call of a builtin;
//   - the ready call otherwise.
func parseCall(content string) (*call, error) {
	expr, err := parser.ParseExpr(content)
	if err != nil {
		if startsWithBuiltin(content) {
			return nil, fmt.Errorf("cannot parse %q: %v", content, err)
		}
		return nil, nil
	}
	ce, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, nil
	}
	return parseCallExpr(content, ce)
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
	path, ok := args[0].(string)
	if !ok {
		panic(fmt.Sprintf("%s: path %s is %T, not string", c.text, c.args[0].text, args[0]))
	}
	if e.depth+1 > e.t.IncludeLimit {
		e.t.Logger.Panicf("Limit of %d nested Exec calls reached while trying to execute %s",
			e.t.IncludeLimit, path)
	}
	n, err := e.t.exec(w, path, e.stash, e.depth+1)
	return int(n), err
}
