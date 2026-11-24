package gledki

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	ct1, ok := tpls.compiled.load(fullPath)
	if !ok || ct1.tpl == nil {
		t.Fatalf("no parsed template in memory: %v %#v", ok, ct1)
	}
	if got := execute(t, tpls, "page", Stash{"title": "B"}); got != "<p>B</p>" {
		t.Fatalf("got %q", got)
	}
	if ct2, _ := tpls.compiled.load(fullPath); ct1 != ct2 {
		t.Fatal("the parsed template must be reused")
	}
	tpls.compiled.delete(fullPath)
	if got := execute(t, tpls, "page", Stash{"title": "C"}); got != "<p>C</p>" {
		t.Fatalf("from the compiled file: got %q", got)
	}
	if ct3, ok := tpls.compiled.load(fullPath); !ok || ct3.tpl == nil {
		t.Fatal("the compiled file must be parsed and kept in memory")
	}
}

// TestCompileUnclosedTag checks that a start tag without an end tag is an
// error at compile time and nothing is stored.
func TestCompileUnclosedTag(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"page.htm": "<p>${title</p>",
		"main.htm": "A ${include bad}",
		"bad.htm":  "${x",
	})
	tpls := newIn(t, root, tagsPair)
	text, err := tpls.Compile("page")
	if err == nil || text != "" {
		t.Fatalf("expected an error and no text, got %q, %v", text, err)
	}
	fullPath := tpls.toFullPath("page")
	if _, err := os.Stat(fullPath + CompiledSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no compiled file must be written: %v", err)
	}
	if _, ok := tpls.compiled.load(fullPath); ok {
		t.Fatal("nothing must be kept in memory")
	}
	var out strings.Builder
	if _, err := tpls.Execute(&out, "page"); err == nil {
		t.Fatal("Execute: expected an error")
	}
	t.Run("in included file", func(t *testing.T) {
		if _, err := tpls.Compile("main"); err == nil {
			t.Fatal("expected an error")
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
	root := b.TempDir()
	keys := make([]string, 20)
	var page strings.Builder
	page.WriteString("${include part}\n")
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i)
		page.WriteString("<p>${")
		page.WriteString(keys[i])
		page.WriteString("}</p>\n")
	}
	files := map[string]string{"page": page.String(), "part": "<h1>${title}</h1>"}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name+filesExt), []byte(content), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	tpls, err := New([]string{root}, filesExt, tagsPair, false)
	if err != nil {
		b.Fatal(err)
	}
	if _, err = tpls.Execute(io.Discard, "page"); err != nil {
		b.Fatal(err)
	}
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
