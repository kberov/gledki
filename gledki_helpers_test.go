package gledki

import (
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
