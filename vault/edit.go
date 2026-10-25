package vault

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/swiftj/obsidian-mcp/vault/ofm"
)

// EditKind names an edit operation.
type EditKind string

const (
	// EditReplaceSection replaces the body of a heading's section, keeping
	// the heading line and the blank lines around the body.
	EditReplaceSection EditKind = "replace_section"
	// EditInsertAfterHeading inserts text at the start of a section's body.
	EditInsertAfterHeading EditKind = "insert_after_heading"
	// EditAppend adds text at the end of the note.
	EditAppend EditKind = "append"
	// EditPrepend adds text at the start of the body, after the frontmatter.
	EditPrepend EditKind = "prepend"
	// EditReplaceText replaces every exact match of Find in the body; the
	// number of matches must equal Count.
	EditReplaceText EditKind = "replace_text"
	// EditReplaceBlock replaces the block labelled ^Block, keeping its ID.
	EditReplaceBlock EditKind = "replace_block"
)

// EditOp is one edit. Which fields apply depends on Kind.
type EditOp struct {
	Kind    EditKind
	Heading string // heading path, "H1#Sub", for section ops
	Block   string // block ID, with or without "^"
	Find    string // exact text for replace_text
	Count   int    // expected number of matches for replace_text
	Text    string // the new text
}

// EditError reports an edit whose target matched nothing, more than one
// place, or the wrong number of places (V-EDIT-02).
type EditError struct {
	Op         EditKind
	Target     string
	Reason     string
	Candidates []string // where an ambiguous target matched
	Count      int      // replace_text: the actual number of matches
}

func (e *EditError) Error() string {
	msg := fmt.Sprintf("vault: %s %q: %s", e.Op, e.Target, e.Reason)
	if len(e.Candidates) > 0 {
		msg += "; candidates: " + strings.Join(e.Candidates, ", ")
	}
	return msg
}

// Edit applies ops in order to the note at p as byte-range patches: every
// byte outside the edited ranges stays identical (V-EDIT-01). etag must be
// the note's current etag.
func (v *Vault) Edit(ctx context.Context, p, etag string, ops []EditOp, o WriteOptions) (*Result, error) {
	if path.Ext(p) != ".md" {
		return nil, &PathError{Op: "edit", Path: p, Reason: "only notes (.md) can be edited this way"}
	}
	if etag == "" {
		return nil, ErrETagRequired
	}
	return v.replace(ctx, p, etag, o, func(cur []byte) ([]byte, error) { return ApplyEdits(cur, ops) })
}

// ApplyEdits applies ops in order to a note's bytes.
func ApplyEdits(src []byte, ops []EditOp) ([]byte, error) {
	out := src
	for _, op := range ops {
		next, _, err := applyEdit(out, op)
		if err != nil {
			return nil, err
		}
		out = next
	}
	return out, nil
}

// applyEdit applies one op and returns the result and the range of src it
// replaced.
func applyEdit(src []byte, op EditOp) ([]byte, ofm.Span, error) {
	d := ofm.Parse(src)
	eol := lineEnding(src)
	text := toEOL(op.Text, eol)
	switch op.Kind {
	case EditAppend:
		at := len(src)
		ins := text + eol
		if at > d.BodyStart() && src[at-1] != '\n' {
			ins = eol + ins
		}
		return patch(src, ofm.Span{Start: at, End: at}, ins)
	case EditPrepend:
		at := d.BodyStart()
		return patch(src, ofm.Span{Start: at, End: at}, text+eol)
	case EditReplaceSection, EditInsertAfterHeading:
		sec, err := findSection(d, op)
		if err != nil {
			return nil, ofm.Span{}, err
		}
		body := sectionBody(d, sec)
		if op.Kind == EditInsertAfterHeading {
			at := body.Start + leadingBlank(src[body.Start:body.End])
			return patch(src, ofm.Span{Start: at, End: at}, text+eol)
		}
		inner := ofm.Span{Start: body.Start + leadingBlank(src[body.Start:body.End]), End: body.End - trailingBlank(src[body.Start:body.End])}
		if inner.Start > inner.End {
			inner.End = inner.Start
		}
		repl := text
		if repl != "" && (inner.Len() == 0 || src[inner.End-1] == '\n') {
			repl += eol
		}
		return patch(src, inner, repl)
	case EditReplaceText:
		if op.Find == "" {
			return nil, ofm.Span{}, &EditError{Op: op.Kind, Reason: "find text is empty"}
		}
		start := d.BodyStart()
		find := toEOL(op.Find, eol)
		n := bytes.Count(src[start:], []byte(find))
		if n != op.Count || n == 0 {
			return nil, ofm.Span{}, &EditError{Op: op.Kind, Target: op.Find, Reason: fmt.Sprintf("found %d matches, expected %d", n, op.Count), Count: n}
		}
		first := start + bytes.Index(src[start:], []byte(find))
		last := start + bytes.LastIndex(src[start:], []byte(find)) + len(find)
		repl := bytes.ReplaceAll(src[first:last], []byte(find), []byte(text))
		return patch(src, ofm.Span{Start: first, End: last}, string(repl))
	case EditReplaceBlock:
		id := strings.TrimPrefix(strings.TrimSpace(op.Block), "^")
		var hits []ofm.BlockID
		for _, b := range d.BlockIDs {
			if strings.EqualFold(b.ID, id) {
				hits = append(hits, b)
			}
		}
		switch len(hits) {
		case 0:
			return nil, ofm.Span{}, &EditError{Op: op.Kind, Target: "^" + id, Reason: "no block has this ID"}
		case 1:
		default:
			var c []string
			for _, h := range hits {
				c = append(c, fmt.Sprintf("^%s (line %d)", h.ID, lineOf(src, h.Start)))
			}
			return nil, ofm.Span{}, &EditError{Op: op.Kind, Target: "^" + id, Reason: "more than one block has this ID", Candidates: c}
		}
		h := hits[0]
		if h.Start >= h.Block.Start && h.End <= h.Block.End { // the ID is inside the block: keep it
			return patch(src, h.Block, strings.TrimRight(text, "\r\n")+" ^"+h.ID)
		}
		return patch(src, h.Block, strings.TrimRight(text, "\r\n"))
	}
	return nil, ofm.Span{}, &EditError{Op: op.Kind, Reason: "unknown edit operation"}
}

func patch(src []byte, s ofm.Span, text string) ([]byte, ofm.Span, error) {
	out := make([]byte, 0, len(src)-s.Len()+len(text))
	out = append(out, src[:s.Start]...)
	out = append(out, text...)
	out = append(out, src[s.End:]...)
	return out, s, nil
}

// findSection resolves a heading path such as "H1#Sub" (V-EDIT-02). Each
// earlier part must name an enclosing heading, in order. Exact text is tried
// first, then a unique case-insensitive match.
func findSection(d *ofm.Doc, op EditOp) (ofm.Section, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(op.Heading), "#"), "#")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return ofm.Section{}, &EditError{Op: op.Kind, Target: op.Heading, Reason: "heading is empty"}
	}
	for _, fold := range []bool{false, true} {
		eq := func(a, b string) bool { return a == b || (fold && strings.EqualFold(a, b)) }
		var hits []int
		for k, h := range d.Headings {
			if eq(h.Text, parts[len(parts)-1]) && ancestorsMatch(d.Headings, k, parts[:len(parts)-1], eq) {
				hits = append(hits, k)
			}
		}
		switch {
		case len(hits) == 1:
			return d.Sections[hits[0]], nil
		case len(hits) > 1:
			var c []string
			for _, k := range hits {
				c = append(c, fmt.Sprintf("%s (line %d)", headingPath(d.Headings, k), lineOf(d.Src, d.Headings[k].Start)))
			}
			return ofm.Section{}, &EditError{Op: op.Kind, Target: op.Heading, Reason: "more than one heading matches", Candidates: c}
		}
	}
	return ofm.Section{}, &EditError{Op: op.Kind, Target: op.Heading, Reason: "no heading matches"}
}

// ancestors returns the indexes of the headings enclosing heading k,
// outermost first.
func ancestors(hs []ofm.Heading, k int) []int {
	var out []int
	level := hs[k].Level
	for j := k - 1; j >= 0 && level > 1; j-- {
		if hs[j].Level < level {
			out = append([]int{j}, out...)
			level = hs[j].Level
		}
	}
	return out
}

func ancestorsMatch(hs []ofm.Heading, k int, want []string, eq func(a, b string) bool) bool {
	anc := ancestors(hs, k)
	i := 0
	for _, j := range anc {
		if i < len(want) && eq(hs[j].Text, want[i]) {
			i++
		}
	}
	return i == len(want)
}

func headingPath(hs []ofm.Heading, k int) string {
	var parts []string
	for _, j := range ancestors(hs, k) {
		parts = append(parts, hs[j].Text)
	}
	return strings.Join(append(parts, hs[k].Text), "#")
}

// sectionBody is a section without its heading line(s).
func sectionBody(d *ofm.Doc, s ofm.Section) ofm.Span {
	h := d.Headings[s.Heading]
	start := min(skipLineEnd(d.Src, h.End), s.End)
	return ofm.Span{Start: start, End: s.End}
}

func skipLineEnd(src []byte, i int) int {
	if i < len(src) && src[i] == '\r' {
		i++
	}
	if i < len(src) && src[i] == '\n' {
		i++
	}
	return i
}

// leadingBlank and trailingBlank measure the blank lines at either end of b.
func leadingBlank(b []byte) int {
	n := 0
	for {
		k := bytes.IndexByte(b[n:], '\n')
		if k < 0 || len(bytes.TrimSpace(b[n:n+k])) != 0 {
			return n
		}
		n += k + 1
	}
}

func trailingBlank(b []byte) int {
	q := len(bytes.TrimRight(b, " \t\r\n"))
	if q == 0 {
		return 0
	}
	if k := bytes.IndexByte(b[q:], '\n'); k >= 0 {
		return len(b) - (q + k + 1)
	}
	return 0
}

func lineEnding(src []byte) string {
	if usesCRLF(src) {
		return "\r\n"
	}
	return "\n"
}

func toEOL(s, eol string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if eol != "\n" {
		s = strings.ReplaceAll(s, "\n", eol)
	}
	return s
}

func lineOf(src []byte, off int) int {
	return bytes.Count(src[:off], []byte("\n")) + 1
}
