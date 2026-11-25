package gledki

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/gommon/log"
)

// TestExecuteKeepsParsedTemplate checks that the parsed template is kept in
// memory and reused, also when it is loaded from the compiled file on disk.
func TestExecuteKeepsParsedTemplate(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "<p>${title}</p>"})
	tpls := newIn(t, root, tagsPair)
	if got := execute(t, tpls, "page", Stash{"title": "A"}); got != "<p>A</p>" {
		t.Fatalf("got %q", got)
	}
	fullPath := tpls.toFullPath("page")
	compiledT1, ok := tpls.compiled.load(fullPath)
	if !ok || compiledT1.tpl == nil {
		t.Fatalf("no parsed template in memory: %v %#v", ok, compiledT1)
	}
	if got := execute(t, tpls, "page", Stash{"title": "B"}); got != "<p>B</p>" {
		t.Fatalf("got %q", got)
	}
	if compiledT2, _ := tpls.compiled.load(fullPath); compiledT1 != compiledT2 {
		t.Fatal("the parsed template must be reused")
	}
	tpls.compiled.delete(fullPath)
	if got := execute(t, tpls, "page", Stash{"title": "C"}); got != "<p>C</p>" {
		t.Fatalf("from the compiled file: got %q", got)
	}
	if compiledT3, ok := tpls.compiled.load(fullPath); !ok || compiledT3.tpl == nil {
		t.Fatal("the compiled file must be parsed and kept in memory")
	}
}

// TestCompileUnclosedTag checks that a start tag without an end tag works as
// in v0.7.8 (it is written as it is), but is reported with the template file
// and the line, where it is: as a warning by Compile and as a panic by
// MustCompile.
func TestCompileUnclosedTag(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"page.htm": "Price ${x}\n<script>var a = \"${\";</script>",
		"main.htm": "A ${include bad} B ${y}",
		"bad.htm":  "${x",
		"same.htm": "a ${x b ${y}",
	})
	tpls := newIn(t, root, tagsPair)
	var logged strings.Builder
	tpls.Logger = log.New("test")
	tpls.Logger.SetOutput(&logged)
	tpls.Logger.SetLevel(log.WARN)

	text, err := tpls.Compile("page")
	if err != nil || text != "Price ${x}\n<script>var a = \"${\";</script>" {
		t.Fatalf("Compile: got %q, %v", text, err)
	}
	if want := "page.htm, line 2: start tag"; !strings.Contains(logged.String(), want) {
		t.Errorf("the warning must contain %q, got %q", want, logged.String())
	}
	var out strings.Builder
	if _, err := tpls.Exec(&out, "page", Stash{"x": "1"}); err != nil ||
		out.String() != "Price 1\n<script>var a = \"${\";</script>" {
		t.Fatalf("Exec: got %q, %v", out.String(), err)
	}
	if compiledT, ok := tpls.compiled.load(tpls.toFullPath("page")); !ok || compiledT.tpl != nil {
		t.Fatal("the compiled template without a parsed template must be kept in memory")
	}
	if msg := panicMessage(func() { tpls.MustCompile("page") }); !strings.Contains(msg, "page.htm, line 2") {
		t.Errorf("MustCompile must panic with the file and the line, got %q", msg)
	}

	t.Run("in included file", func(t *testing.T) {
		if msg := panicMessage(func() { tpls.MustCompile("main") }); !strings.Contains(msg, "bad.htm, line 1") {
			t.Errorf("MustCompile must name the included file, got %q", msg)
		}
		out.Reset()
		if _, err := tpls.Exec(&out, "main", Stash{"x": "1", "y": "2"}); err != nil || out.String() != "A " {
			t.Fatalf("Exec must work as in v0.7.8, got %q, %v", out.String(), err)
		}
	})
	t.Run("end tag of a later tag", func(t *testing.T) {
		if msg := panicMessage(func() { tpls.MustCompile("same") }); !strings.Contains(msg, "same.htm, line 1") {
			t.Errorf("MustCompile must find it, got %q", msg)
		}
	})
}

// TestCompileBrokenCompiledFile checks that a broken compiled file is not
// used: the template is compiled again and the compiled file is overwritten.
func TestCompileBrokenCompiledFile(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"page.htm":                  "<p>${title}</p>",
		"page.htm" + CompiledSuffix: "<p>${title",
	})
	tpls := newIn(t, root, tagsPair)
	text, err := tpls.Compile("page")
	if err != nil || text != "<p>${title}</p>" {
		t.Fatalf("got %q, %v", text, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "page.htm"+CompiledSuffix))
	if err != nil || string(data) != "<p>${title}</p>" {
		t.Fatalf("the compiled file must be overwritten, got %q, %v", data, err)
	}
}

// TestExecutePlainText checks a template without any tags.
func TestExecutePlainText(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "just text"})
	tpls := newIn(t, root, tagsPair)
	var out strings.Builder
	n, err := tpls.Execute(&out, "page")
	if err != nil || n != 9 || out.String() != "just text" {
		t.Fatalf("got %q, %d, %v", out.String(), n, err)
	}
}

// TestExecuteOtherTags checks that the tags of the instance are used for
// parsing.
func TestExecuteOtherTags(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "<p>{{title}}</p> ${title}"})
	tpls := newIn(t, root, [2]string{"{{", "}}"})
	if got := execute(t, tpls, "page", Stash{"title": "A"}); got != "<p>A</p> ${title}" {
		t.Fatalf("got %q", got)
	}
}

// TestExecuteCacheTemplatesFalse checks that with CacheTemplates false
// Execute sees a change in an included file at once and keeps nothing.
func TestExecuteCacheTemplatesFalse(t *testing.T) {
	CacheTemplates = false
	t.Cleanup(func() { CacheTemplates = true })
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "A ${include part}", "part.htm": "1"})
	tpls := newIn(t, root, tagsPair)
	if got := execute(t, tpls, "page", Stash{}); got != "A 1" {
		t.Fatalf("got %q", got)
	}
	writeFiles(t, root, map[string]string{"part.htm": "2"})
	if got := execute(t, tpls, "page", Stash{}); got != "A 2" {
		t.Fatalf("got %q", got)
	}
	if _, ok := tpls.compiled.load(tpls.toFullPath("page")); ok {
		t.Fatal("nothing must be kept in memory")
	}
}

// BenchmarkExecute measures [Gledki.Execute] of an already compiled template
// with one included file and 21 placeholders. The template is compiled before
// the loop, so only the execution is measured. The stash is filled again in
// every iteration, because Execute clears it.
func BenchmarkExecute(b *testing.B) {
	tpls, keys := benchTemplate(b)
	b.ReportAllocs()
	for b.Loop() {
		tpls.Stash["title"] = "T"
		for i := range keys {
			tpls.Stash[keys[i]] = "v"
		}
		if _, err := tpls.Execute(io.Discard, "page"); err != nil {
			b.Fatal(err)
		}
	}
}

// TestMustCompile checks that MustCompile returns the compiled text and
// panics on any error, so that a broken template is seen at once during
// development.
func TestMustCompile(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"page.htm": "<p>${title}</p>",
		"bad.htm":  "<p>${title</p>",
	})

	tpls := newIn(t, root, tagsPair)
	if got := tpls.MustCompile("page"); got != "<p>${title}</p>" {
		t.Fatalf("got %q", got)
	}
	expectPanic(t, func() { tpls.MustCompile("bad") })
	expectPanic(t, func() { tpls.MustCompile("nosuchfile") })
}
