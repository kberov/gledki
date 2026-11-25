package gledki

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/valyala/fasttemplate"
)

// lineMarker is written in place of a tag, which writes nothing (a comment
// or `wrapper`). A TagFunc sees only its tag, not the line around it, so it
// cannot know whether the tag stands alone on its line. The marker lets
// [removeMarkers] decide this after the pass: a line, which contains only
// markers and spaces, held only such tags and is removed together with its
// new line, so that no empty line is left; in any other line only the
// markers are removed. NUL is used, because it does not occur in template
// texts; a template text with a NUL byte is an error, so that no NUL of the
// template text is taken for a marker.
const lineMarker = "\x00"

// preprocess makes the compiled text from the template text `text` of the
// template file `fullPath` in one pass over its tags, done by fasttemplate
// itself, so that the tags are found exactly as when the template is
// executed. For every tag the spaces around its content are removed and:
//   - `# …` (a comment) writes nothing;
//   - `wrapper some/file` writes nothing and remembers the wrapper file;
//   - `include some/file` writes the processed template text of the file;
//   - any other tag is written back as a placeholder without the spaces.
//
// When there is a wrapper, the result is put in place of the placeholders
// `content` of the wrapper file itself, while the wrapper file is processed;
// a `content` in a file included into the wrapper file stays a placeholder.
// `depth` is how deep the template file is included (0 for the main template
// file).
//
// A template text with a NUL byte is an error (see [lineMarker]). An error of
// reading an included or a wrapper file names the template file and the line
// of the directive.
//
// TODO: dot notation like `${keyFromStash.Property}` and the directive
// `${execute path/to/file}`. Decide whether they are resolved at compile time
// (a private stash of the file) or at execution time.
func (t *Gledki) preprocess(fullPath, text string, depth int, tCheck *tagCheck) (string, error) {
	p := &preprocessor{t: t, fullPath: fullPath, depth: depth, tCheck: tCheck}
	return p.run(text)
}

// run does the work of [Gledki.preprocess] for the template text `text`.
func (p *preprocessor) run(text string) (string, error) {
	t := p.t
	p.text = text
	if i := strings.IndexByte(text, 0); i >= 0 {
		return "", p.errorf(i, "NUL byte")
	}
	p.tCheck.check(t, p.fullPath, text)
	p.text = strings.TrimSuffix(text, "\n")
	out, err := fasttemplate.ExecuteFuncStringWithErr(p.text, t.Tags[0], t.Tags[1], p.tag)
	if err != nil {
		return "", err
	}
	return p.wrap(removeMarkers(out))
}

// preprocessor keeps the state of one pass of [Gledki.preprocess] over the
// tags of one template file. Its methods handle the different kinds of tags.
type preprocessor struct {
	t         *Gledki
	fullPath  string // the template file
	text      string // its template text
	tagAt     int    // where the current tag starts in text
	next      int    // where the search for the next tag starts in text
	depth     int    // how deep the template file is included
	tCheck    *tagCheck
	wrapper   string // the argument of the first `wrapper` found, or ""
	wrapperAt int    // where the first `wrapper` starts in text
	// isWrapper is true, when the template file is the wrapper file of
	// `content`, the already processed text of the file it wraps.
	isWrapper bool
	content   string
}

// tag is the [fasttemplate.TagFunc] of the pass. It removes the spaces around
// the content of the tag and chooses the method, which handles it.
func (p *preprocessor) tag(w io.Writer, tag string) (int, error) {
	p.findTag(tag)
	content := strings.TrimSpace(tag)
	name, arg := directive(content)
	switch {
	case strings.HasPrefix(content, "#"):
		return p.comment(w)
	case name == "wrapper" && arg != "":
		return p.setWrapper(w, arg)
	case name == "include" && arg != "":
		return p.include(w, arg)
	case p.isWrapper && content == "content":
		return io.WriteString(w, p.content)
	default:
		return p.placeholder(w, content)
	}
}

// findTag sets tagAt to where the tag with the content `tag` (as fasttemplate
// passes it, with the spaces) starts in the template text. fasttemplate calls
// the TagFunc for the tags in their order, so the search starts after the
// previous tag, and the same tag found twice gets two different places.
func (p *preprocessor) findTag(tag string) {
	whole := p.t.Tags[0] + tag + p.t.Tags[1]
	if i := strings.Index(p.text[p.next:], whole); i >= 0 {
		p.tagAt = p.next + i
		p.next = p.tagAt + len(whole)
	}
}

// lineAt returns the number of the line (from 1) in the template text, where
// the byte at `at` is.
func (p *preprocessor) lineAt(at int) int {
	return 1 + strings.Count(p.text[:at], "\n")
}

// errorf returns an error, which starts with the template file and the line
// of the byte at `at`, followed by `format` with `args`, as by [fmt.Errorf].
func (p *preprocessor) errorf(at int, format string, args ...any) error {
	return fmt.Errorf("template file %s, line %d: "+format,
		append([]any{p.fullPath, p.lineAt(at)}, args...)...)
}

// comment writes a [lineMarker] in place of a comment `# …`.
func (p *preprocessor) comment(w io.Writer) (int, error) {
	return io.WriteString(w, lineMarker)
}

// setWrapper remembers `path` as the wrapper file, if no wrapper was found
// before in the same file; a later `wrapper` in the same file is ignored and
// logged as a warning with the template file and the line. In both cases it
// writes a [lineMarker] in place of the tag, so the tag is removed like a
// comment.
func (p *preprocessor) setWrapper(w io.Writer, path string) (int, error) {
	if p.wrapper == "" {
		p.wrapper, p.wrapperAt = path, p.tagAt
	} else {
		p.t.Logger.Warn(p.errorf(p.tagAt, "wrapper %s ignored, the wrapper is %s", path, p.wrapper))
	}
	return io.WriteString(w, lineMarker)
}

// include writes the preprocessed template text of the file `path` in place
// of the tag. The file is one level deeper than the file, which includes it
// (see [preprocessor.checkLimit]). An error names the template file and the
// line of the tag.
func (p *preprocessor) include(w io.Writer, path string) (int, error) {
	p.checkLimit("include", path)
	t := p.t
	text, err := t.LoadFile(path)
	if err == nil {
		text, err = t.preprocess(t.toFullPath(path), text, p.depth+1, p.tCheck)
	}
	if err != nil {
		return 0, p.errorf(p.tagAt, "include %s: %w", path, err)
	}
	return io.WriteString(w, text)
}

// checkLimit panics, when the file `path`, named by a directive (`action`
// is "include" or "wrap in"), would be deeper than [Gledki.IncludeLimit].
// Every `include` and every `wrapper` puts the file it names one level
// deeper along the chain from the main template file (depth 0).
func (p *preprocessor) checkLimit(action, path string) {
	if p.depth+1 > p.t.IncludeLimit {
		p.t.Logger.Panicf("Limit of %d nested inclusions or wrappers reached"+
			" while trying to %s %s", p.t.IncludeLimit, action, path)
	}
}

// placeholder writes the tag back with its trimmed content, so it is
// replaced by a value from the [Stash] at execution time.
func (p *preprocessor) placeholder(w io.Writer, content string) (int, error) {
	return io.WriteString(w, p.t.Tags[0]+content+p.t.Tags[1])
}

// wrap returns `text` as it is, when no wrapper was found. Otherwise it
// processes the wrapper file, which puts `text` in place of its own
// placeholders `content`. The wrapper file is considered one level deeper than
// the wrapped file (see [preprocessor.checkLimit]), so a wrapper file can have
// its own wrapper, and a cycle of wrappers panics instead of never ending.  An
// error of reading the wrapper file names the template file and the line of
// the `wrapper` tag.
func (p *preprocessor) wrap(text string) (string, error) {
	if p.wrapper == "" {
		return text, nil
	}
	p.checkLimit("wrap in", p.wrapper)
	t := p.t
	wrapperText, err := t.LoadFile(p.wrapper)
	if err != nil {
		return "", p.errorf(p.wrapperAt, "wrapper %s: %w", p.wrapper, err)
	}
	wp := &preprocessor{t: t, fullPath: t.toFullPath(p.wrapper), depth: p.depth + 1,
		tCheck: p.tCheck, isWrapper: true, content: text}
	return wp.run(wrapperText)
}

// directive separates the trimmed content of a tag into the name of a
// directive and its argument. The name ends at the first white space – a
// space, a tab or a new line – and the argument is the rest without the
// spaces around it. Content without white space is only a name.
func directive(content string) (name, arg string) {
	i := strings.IndexFunc(content, unicode.IsSpace)
	if i < 0 {
		return content, ""
	}
	return content[:i], strings.TrimSpace(content[i:])
}

// removeMarkers removes every line, which contains only [lineMarker]s and
// spaces, with its new line, and the remaining markers.
func removeMarkers(text string) string {
	if !strings.Contains(text, lineMarker) {
		return text
	}
	var b strings.Builder
	for line := range strings.SplitAfterSeq(text, "\n") {
		if strings.Contains(line, lineMarker) &&
			strings.Trim(line, lineMarker+" \t\r\n") == "" {
			continue
		}
		b.WriteString(strings.ReplaceAll(line, lineMarker, ""))
	}
	return b.String()
}
