package gledki

import (
	"strings"
	"testing"

	"github.com/labstack/gommon/log"
)

// TestDirective checks that the name of a directive is separated from its
// argument by any white space – a space, a tab or a new line.
func TestDirective(t *testing.T) {
	for _, c := range []struct{ in, name, arg string }{
		{"include p", "include", "p"},
		{"include\n    p", "include", "p"},
		{"wrapper\tw", "wrapper", "w"},
		{"include      \n  p/q", "include", "p/q"},
		{"title", "title", ""},
		{"# a comment", "#", "a comment"},
	} {
		name, arg := directive(c.in)
		if name != c.name || arg != c.arg {
			t.Errorf("directive(%q) = %q, %q; want %q, %q", c.in, name, arg, c.name, c.arg)
		}
	}
}

// preprocessCase is one case of [TestPreprocess]: the template files to
// write (the main one is always `page.htm`), the stash to execute `page`
// with and the expected output.
type preprocessCase struct {
	name  string
	files map[string]string
	stash Stash
	want  string
}

// preprocessCases are the cases of [TestPreprocess], one for every kind of
// tag.
var preprocessCases = []preprocessCase{
	{
		name: "all kinds of tags",
		files: map[string]string{
			"page.htm": "${ wrapper  w }\n<ul>\n  ${# filled by a TagFunc }\n${ items }\n</ul> ${#x} ${ include  p }",
			"w.htm":    "<body>${ content }</body>",
			"p.htm":    "[${  title  }]",
		},
		stash: Stash{"items": "I", "title": "T"},
		want:  "<body><ul>\nI\n</ul>  [T]</body>",
	},
	{
		name:  "comment alone on its line",
		files: map[string]string{"page.htm": "a\n  ${# c }\nb"},
		want:  "a\nb",
	},
	{
		name:  "comment inside a line",
		files: map[string]string{"page.htm": "a ${#x} b"},
		want:  "a  b",
	},
	{
		name:  "placeholder with new lines",
		files: map[string]string{"page.htm": "${  title\n }"},
		stash: Stash{"title": "T"},
		want:  "T",
	},
	{
		name: "second wrapper ignored",
		files: map[string]string{
			"page.htm": "${wrapper w}\n${wrapper v}\nX",
			"w.htm":    "<${content}>",
			"v.htm":    "{${content}}",
		},
		want: "<X>",
	},
	{
		name:  "wrapper without argument",
		files: map[string]string{"page.htm": "${ wrapper }"},
		stash: Stash{"wrapper": "W"},
		want:  "W",
	},
	{
		name: "included file with its own wrapper",
		files: map[string]string{
			"page.htm": "[${include p}]",
			"p.htm":    "${wrapper w}\nP",
			"w.htm":    "<${content}>",
		},
		want: "[<P>]",
	},
	{
		name: "content in a file included into a wrapper",
		files: map[string]string{
			"page.htm": "${wrapper w}\nX",
			"w.htm":    "<${ content }${include p}>",
			"p.htm":    "P${content}",
		},
		stash: Stash{"content": "S"},
		want:  "<XPS>",
	},
}

// TestPreprocess checks every kind of tag, handled in one pass by
// [Gledki.preprocess]: comments, `wrapper`, `include` and placeholders.
func TestPreprocess(t *testing.T) {
	for _, c := range preprocessCases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, c.files)
			if c.stash == nil {
				c.stash = Stash{}
			}
			got := execute(t, newIn(t, root, tagsPair), "page", c.stash)
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestPreprocessNewLineAfterName checks that a file is included, when a new
// line follows the name `include` directly.
func TestPreprocessNewLineAfterName(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "a${include\np}b", "p.htm": "P"})
	if got := execute(t, newIn(t, root, tagsPair), "page", Stash{}); got != "aPb" {
		t.Fatalf("got %q, want %q", got, "aPb")
	}
}

// limitCase is one case of [TestNestingLimit]: the template files to write
// (the main one is always `page.htm`) and the expected output, or whether
// compiling `page` panics, because [Gledki.IncludeLimit] is reached.
type limitCase struct {
	name   string
	files  map[string]string
	want   string
	panics bool
}

// limitCases are the cases of [TestNestingLimit] with IncludeLimit 3: every
// `wrapper` and every `include` puts the file it names one level deeper.
var limitCases = []limitCase{
	{
		name: "three nested wrappers",
		files: map[string]string{
			"page.htm": "${wrapper w}\nX",
			"w.htm":    "${wrapper v}\n<${content}>",
			"v.htm":    "${wrapper base}\n{${content}}",
			"base.htm": "[${content}]",
		},
		want: "[{<X>}]",
	},
	{
		name: "four nested wrappers",
		files: map[string]string{
			"page.htm": "${wrapper w}\nX",
			"w.htm":    "${wrapper v}\n<${content}>",
			"v.htm":    "${wrapper base}\n{${content}}",
			"base.htm": "${wrapper root}\n[${content}]",
			"root.htm": "(${content})",
		},
		panics: true,
	},
	{
		name: "wrapper including three levels",
		files: map[string]string{
			"page.htm": "${wrapper w}\nX",
			"w.htm":    "<${content}${include a}>",
			"a.htm":    "${include b}",
			"b.htm":    "${include c}",
			"c.htm":    "C",
		},
		panics: true,
	},
	{
		name:   "wrapper of itself",
		files:  map[string]string{"page.htm": "${wrapper page}\nX"},
		panics: true,
	},
	{
		name: "wrappers of each other",
		files: map[string]string{
			"page.htm": "${wrapper w}\nX",
			"w.htm":    "${wrapper page}\n<${content}>",
		},
		panics: true,
	},
}

// TestNestingLimit checks that wrappers and inclusions are counted together
// against [Gledki.IncludeLimit] along the chain from the main template file,
// so that a cycle of wrappers panics instead of overflowing the stack.
func TestNestingLimit(t *testing.T) {
	for _, c := range limitCases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, c.files)
			tpls := newIn(t, root, tagsPair)
			tpls.IncludeLimit = 3
			if c.panics {
				expectPanic(t, func() { _, _ = tpls.Compile("page") })
				return
			}
			if got := execute(t, tpls, "page", Stash{}); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestSecondWrapperWarning checks that a second `wrapper` in a template file
// is logged as a warning with the template file and the line.
func TestSecondWrapperWarning(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"page.htm": "${wrapper w}\nX\n${ wrapper v }\nY",
		"same.htm": "${wrapper w}\n${wrapper w}\nZ",
		"w.htm":    "<${content}>",
		"v.htm":    "{${content}}",
	})
	tpls := newIn(t, root, tagsPair)
	var logged strings.Builder
	tpls.Logger = log.New("test")
	tpls.Logger.SetOutput(&logged)
	tpls.Logger.SetLevel(log.WARN)
	for _, c := range []struct{ path, want string }{
		{"page", "page.htm, line 3: wrapper v ignored, the wrapper is w"},
		{"same", "same.htm, line 2: wrapper w ignored, the wrapper is w"},
	} {
		logged.Reset()
		if _, err := tpls.Compile(c.path); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(logged.String(), c.want) {
			t.Errorf("the warning must contain %q, got %q", c.want, logged.String())
		}
	}
}

// compileErrorCases are the cases of [TestCompileErrors]: the template files
// to write and what the error of compiling `page` must contain.
var compileErrorCases = []struct{ name, page, want string }{
	{"NUL byte", "a\n\x00\nb", "page.htm, line 2: NUL byte"},
	{"missing included file", "a\n${include missing}", "page.htm, line 2: include missing:"},
	{"include with two arguments", "${include p q}", "page.htm, line 1: include p q:"},
	{"missing wrapper file", "${wrapper nowrap}\nX", "page.htm, line 1: wrapper nowrap:"},
}

// TestCompileErrors checks that the errors of preprocessing name the template
// file and the line.
func TestCompileErrors(t *testing.T) {
	for _, c := range compileErrorCases {
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
