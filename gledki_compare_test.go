package gledki

import (
	htmltemplate "html/template"
	"io"
	"strings"
	"testing"
	texttemplate "text/template"
)

// compareCase is one page of [BenchmarkCompare], written for each engine.
// The pages give the same output, which [TestCompareSameOutput] checks.
type compareCase struct {
	name string
	// gledki are the template files for Gledki; the main one is page.htm.
	gledki map[string]string
	// std is the template for text/template and html/template.
	std string
}

// compareStash is the data of every page of [compareCases]. It has no
// character, which html/template escapes, so all engines write the same.
var compareStash = Stash{"u": true, "t": "1", "v": "value"}

// compareFuncs are the functions of text/template and html/template; pick
// is the same function, which the tests register for Gledki as Pick.
var compareFuncs = map[string]any{"pick": pick}

var compareCases = []compareCase{
	{name: "Values",
		gledki: page(strings.Repeat("<p>${v}</p>", 20)),
		std:    strings.Repeat("<p>{{.v}}</p>", 20)},
	{name: "IfElse",
		gledki: page(strings.Repeat("<p>${IfElse(u, `a`, `b`)}</p>", 20)),
		std:    strings.Repeat("<p>{{if .u}}a{{else}}b{{end}}</p>", 20)},
	{name: "Func",
		gledki: page(strings.Repeat("<option ${Pick(`t`, `1`)}>", 20)),
		std:    strings.Repeat(`<option {{pick . "t" "1"}}>`, 20)},
	{name: "Exec",
		gledki: map[string]string{
			"page.htm": "<ul>" + strings.Repeat("${Exec(`part`)}", 5) + "</ul>",
			"part.htm": "<li>${v}</li>"},
		std: `{{define "part"}}<li>{{.v}}</li>{{end}}<ul>` +
			strings.Repeat(`{{template "part" .}}`, 5) + "</ul>"},
	{name: "Include",
		gledki: map[string]string{
			"page.htm": "<ul>" + strings.Repeat("${include part}", 5) + "</ul>",
			"part.htm": "<li>${v}</li>"},
		std: `{{define "part"}}<li>{{.v}}</li>{{end}}<ul>` +
			strings.Repeat(`{{template "part" .}}`, 5) + "</ul>"},
}

// compareEngine executes one page of [compareCases] with one engine.
type compareEngine struct {
	name string
	// prepare compiles the page `c` in `tb` and returns a function, which
	// executes it to `w`.
	prepare func(tb testing.TB, c compareCase) func(w io.Writer) error
}

var compareEngines = []compareEngine{
	{"Gledki", func(tb testing.TB, c compareCase) func(io.Writer) error {
		root := tb.TempDir()
		writeFiles(tb, root, c.gledki)
		tpls := newIn(tb, root, tagsPair)
		return func(w io.Writer) error {
			_, err := tpls.Exec(w, "page", compareStash)
			return err
		}
	}},
	{"text", func(tb testing.TB, c compareCase) func(io.Writer) error {
		t, err := texttemplate.New("page").Funcs(compareFuncs).Parse(c.std)
		if err != nil {
			tb.Fatal(err)
		}
		return func(w io.Writer) error { return t.Execute(w, compareStash) }
	}},
	{"html", func(tb testing.TB, c compareCase) func(io.Writer) error {
		t, err := htmltemplate.New("page").Funcs(compareFuncs).Parse(c.std)
		if err != nil {
			tb.Fatal(err)
		}
		return func(w io.Writer) error { return t.Execute(w, compareStash) }
	}},
}

// TestCompareSameOutput checks that all engines write the same output for
// each page of [BenchmarkCompare], so they do the same work.
func TestCompareSameOutput(t *testing.T) {
	for _, c := range compareCases {
		t.Run(c.name, func(t *testing.T) {
			var want string
			for i, e := range compareEngines {
				var out strings.Builder
				if err := e.prepare(t, c)(&out); err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					want = out.String()
					continue
				}
				if out.String() != want {
					t.Fatalf("%s writes %q, %s writes %q", e.name, out.String(), compareEngines[0].name, want)
				}
			}
		})
	}
}

// BenchmarkCompare compares Gledki with text/template and html/template on
// the same pages: 20 values, 20 choices, 20 calls of a function and 5 parts
// (Gledki with Exec and with include). html/template also escapes every value,
// which Gledki and text/template do not.
//
//	go test -run XXX -bench Compare -count 6 . | tee compare.txt
//	benchstat -col /engine compare.txt
func BenchmarkCompare(b *testing.B) {
	for _, c := range compareCases {
		for _, e := range compareEngines {
			b.Run("page="+c.name+"/engine="+e.name, func(b *testing.B) {
				exec := e.prepare(b, c)
				if err := exec(io.Discard); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					if err := exec(io.Discard); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
