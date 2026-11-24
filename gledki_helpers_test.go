package gledki

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles writes the files `files` (file name => content) into `root`.
func writeFiles(tb testing.TB, root string, files map[string]string) {
	tb.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			tb.Fatal(err)
		}
	}
}

// newIn returns a new Gledki with the only root `root` and the tags `tags`.
func newIn(tb testing.TB, root string, tags [2]string) *Gledki {
	tb.Helper()
	tpls, err := New([]string{root}, filesExt, tags, false)
	if err != nil {
		tb.Fatal(err)
	}
	tpls.Logger = logger
	return tpls
}

// execute executes `path` with `stash` and returns the output.
func execute(tb testing.TB, tpls *Gledki, path string, stash Stash) string {
	tb.Helper()
	var out strings.Builder
	tpls.Stash = stash
	if _, err := tpls.Execute(&out, path); err != nil {
		tb.Fatal(err)
	}
	return out.String()
}

// benchTemplate prepares the template "page" for the benchmarks: one included
// file and 21 placeholders – `title` and the returned keys. The template is
// executed once, so that the benchmarks measure only the execution.
func benchTemplate(b *testing.B) (*Gledki, []string) {
	b.Helper()
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
	writeFiles(b, root, map[string]string{
		"page" + filesExt: page.String(),
		"part" + filesExt: "<h1>${title}</h1>",
	})
	tpls := newIn(b, root, tagsPair)
	if _, err := tpls.Execute(io.Discard, "page"); err != nil {
		b.Fatal(err)
	}
	return tpls, keys
}
