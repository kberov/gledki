package gledki

import (
	"fmt"
	"strings"
	"sync"

	"github.com/valyala/fasttemplate"
)

// compiledTemplate is a compiled template (see Terms in the package
// documentation): the compiled text and its parsed template. The parsing
// (finding every placeholder) is done only once per template file, so that
// executing it ([Gledki.Exec]) only writes the output. The parsed template
// does not copy the compiled text: its parts are parts of the compiled text.
//
// If a start tag has no end tag, the compiled text cannot be parsed. Then
// `tpl` is nil and the compiled text is executed as in v0.7.8, without
// parsing it in advance: such a start tag is written as it is.
type compiledTemplate struct {
	text string
	tpl  *fasttemplate.Template
	// tagErr is the first problem, found in the template texts while
	// compiling – a start tag without an end tag or a call of an unknown
	// function – with the template file and the line.
	// It is nil, when the compiled template is taken from a compiled file,
	// which can be parsed.
	tagErr error
}

// compiledMap keeps the compiled templates by the full path of their template
// files. Like [filesMap] it keeps the entries in a [sync.Map], because one
// [Gledki] instance is often used by many goroutines at the same time, and
// every entry is written once and then only read. The zero value is ready for
// use. Do not copy a compiledMap after first use.
type compiledMap struct {
	m sync.Map
}

// load returns the compiled template stored for `path` and true, or nil and
// false if nothing is stored for it.
func (c *compiledMap) load(path string) (*compiledTemplate, bool) {
	compiledT, ok := c.m.Load(path)
	if !ok {
		return nil, false
	}
	return compiledT.(*compiledTemplate), true
}

// store keeps `compiledT` as the compiled template for `path`.
func (c *compiledMap) store(path string, compiledT *compiledTemplate) {
	c.m.Store(path, compiledT)
}

// delete forgets the compiled template for `path`.
func (c *compiledMap) delete(path string) {
	c.m.Delete(path)
}

// parse parses the compiled text `text` into a parsed template with the
// [Gledki.Tags] of this instance and returns both as a compiled template. If a
// start tag has no end tag, the parsed template is nil.
func (t *Gledki) parse(text string) *compiledTemplate {
	compiledT := &compiledTemplate{text: text}
	if tpl, err := fasttemplate.NewTemplate(text, t.Tags[0], t.Tags[1]); err == nil {
		compiledT.tpl = tpl
	}
	return compiledT
}

// tagCheck keeps the first problem, found in the template texts of one
// compilation (the main template file, its wrapper and the included files): a
// start tag without an end tag or a call of an unknown function. It is not an
// error: [Gledki.Compile] logs it as a warning and [Gledki.MustCompile] panics
// with it. One tagCheck is used by one compilation only.
type tagCheck struct {
	err error
}

// note keeps `err` as the problem of the compilation, if no problem is kept
// yet.
func (tCheck *tagCheck) note(err error) {
	if tCheck.err == nil {
		tCheck.err = err
	}
}

// check looks in the template text `text` of the template file `fullPath`
// for a start tag without an end tag. A start tag is without an end tag also
// when another start tag comes before its end tag (`a ${x b ${y}`): then
// fasttemplate would take `x b ${y` as the name of one placeholder. Only the
// first such start tag of a compilation is kept.
func (tCheck *tagCheck) check(t *Gledki, fullPath, text string) {
	if tCheck.err != nil {
		return
	}
	start, end := t.Tags[0], t.Tags[1]
	for offset := 0; ; {
		i := strings.Index(text[offset:], start)
		if i < 0 {
			return
		}
		i += offset
		rest := text[i+len(start):]
		j := strings.Index(rest, end)
		if j < 0 || strings.Contains(rest[:j], start) {
			tCheck.err = fmt.Errorf("template file %s, line %d: start tag %q without end tag",
				fullPath, 1+strings.Count(text[:i], "\n"), start)
			return
		}
		offset = i + len(start) + j + len(end)
	}
}
