package gledki

import (
	"io"
	"slices"
	"strings"
	"testing"
)

// stringWriterRecorder is a writer, which implements [io.StringWriter] and
// remembers what was written with Write and what with WriteString.
type stringWriterRecorder struct {
	out          strings.Builder
	written      []string
	writtenAsStr []string
}

func (r *stringWriterRecorder) Write(p []byte) (int, error) {
	r.written = append(r.written, string(p))
	return r.out.Write(p)
}

func (r *stringWriterRecorder) WriteString(s string) (int, error) {
	r.writtenAsStr = append(r.writtenAsStr, s)
	return r.out.WriteString(s)
}

// TestExecuteWritesStringsWithoutCopy checks that a string value is written
// with WriteString, when the writer has it, so that it is not copied into a
// new []byte.
func TestExecuteWritesStringsWithoutCopy(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "${a}${b}${c}${missing}"})
	tpls := newIn(t, root, tagsPair)
	tpls.Stash = Stash{
		"a": "1",
		"b": []byte("2"),
		"c": TagFunc(func(w io.Writer, _ string) (int, error) { return w.Write([]byte("3")) }),
	}
	out := &stringWriterRecorder{}
	n, err := tpls.Execute(out, "page")
	if err != nil || n != 3 || out.out.String() != "123" {
		t.Fatalf("got %q, %d, %v", out.out.String(), n, err)
	}
	if !slices.Contains(out.writtenAsStr, "1") {
		t.Errorf("the string value must be written with WriteString: %q", out.writtenAsStr)
	}
	if slices.Contains(out.written, "1") {
		t.Errorf("the string value must not be written with Write: %q", out.written)
	}
}

// TestExecuteUnknownValueType checks that a value of an unexpected type
// panics, as in fasttemplate.
func TestExecuteUnknownValueType(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"page.htm": "${a}"})
	tpls := newIn(t, root, tagsPair)
	tpls.Stash = Stash{"a": 42}
	var out strings.Builder
	expectPanic(t, func() { _, _ = tpls.Execute(&out, "page") })
}
