package gledki

import (
	"fmt"
	"sync"

	"github.com/valyala/fasttemplate"
)

// compiledTemplate is a compiled template (see Terms in the package
// documentation): the compiled text and its parsed template. The parsing
// (finding every placeholder) is done only once per template file, so that
// [Gledki.Execute] only writes the output. The parsed template does not copy
// the compiled text: its parts are parts of the compiled text.
type compiledTemplate struct {
	text string
	tpl  *fasttemplate.Template
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
	ct, ok := c.m.Load(path)
	if !ok {
		return nil, false
	}
	return ct.(*compiledTemplate), true
}

// store keeps `ct` as the compiled template for `path`.
func (c *compiledMap) store(path string, ct *compiledTemplate) {
	c.m.Store(path, ct)
}

// delete forgets the compiled template for `path`.
func (c *compiledMap) delete(path string) {
	c.m.Delete(path)
}

// parse parses the compiled text `text` into a parsed template with the
// [Gledki.Tags] of this instance and returns both as a compiled template.
// `fullPath` is the template file or the compiled file, from which `text`
// comes; it is used only in the error message. It returns an error, if a
// start tag has no end tag.
func (t *Gledki) parse(fullPath, text string) (*compiledTemplate, error) {
	tpl, err := fasttemplate.NewTemplate(text, t.Tags[0], t.Tags[1])
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", fullPath, err)
	}
	return &compiledTemplate{text: text, tpl: tpl}, nil
}
