package gledki

import (
	"io"
	"maps"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestExecUsesPassedStash checks that Exec uses only the passed stash and
// does not use, change or clear [Gledki.Stash].
func TestExecUsesPassedStash(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "<p>${title}</p>"})
	tpls := newIn(t, root, tagsPair)
	tpls.Stash = Stash{"title": "shared"}
	var out strings.Builder
	if _, err := tpls.Exec(&out, "page", Stash{"title": "A"}); err != nil || out.String() != "<p>A</p>" {
		t.Fatalf("got %q, %v", out.String(), err)
	}
	if !maps.Equal(tpls.Stash, Stash{"title": "shared"}) {
		t.Fatalf("Gledki.Stash must stay as it was, got %v", tpls.Stash)
	}
}

// TestExecConcurrently checks that one Gledki can execute the same template in
// many goroutines at the same time, each with its own stash. Run it with
// -race.
func TestExecConcurrently(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "<p>${title}</p>"})
	tpls := newIn(t, root, tagsPair)
	outs := make([]strings.Builder, 50) // every goroutine writes only to its own slot
	errs := make([]error, len(outs))
	var wg sync.WaitGroup
	for i := range outs {
		wg.Go(func() {
			_, errs[i] = tpls.Exec(&outs[i], "page", Stash{"title": strconv.Itoa(i)})
		})
	}
	wg.Wait()
	for i := range outs {
		if want := "<p>" + strconv.Itoa(i) + "</p>"; errs[i] != nil || outs[i].String() != want {
			t.Fatalf("goroutine %d: got %q, %v; want %q", i, outs[i].String(), errs[i], want)
		}
	}
}

// TestExecuteClearsStash checks that Execute still uses [Gledki.Stash] and
// clears it after the execution.
func TestExecuteClearsStash(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "<p>${title}</p>"})
	tpls := newIn(t, root, tagsPair)
	if got := execute(t, tpls, "page", Stash{"title": "A"}); got != "<p>A</p>" {
		t.Fatalf("got %q", got)
	}
	if len(tpls.Stash) != 0 {
		t.Fatalf("Gledki.Stash must be cleared, got %v", tpls.Stash)
	}
}

// BenchmarkExecParallel measures [Gledki.Exec] of the template of
// [BenchmarkExecute] in many goroutines at the same time, each with its own
// stash.
func BenchmarkExecParallel(b *testing.B) {
	tpls, keys := benchTemplate(b)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		stash := make(Stash, len(keys)+1)
		stash["title"] = "T"
		for _, k := range keys {
			stash[k] = "v"
		}
		for pb.Next() {
			if _, err := tpls.Exec(io.Discard, "page", stash); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
