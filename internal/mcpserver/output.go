package mcpserver

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Output limits [policy D-011].
const (
	defaultLimit = 50
	maxLimit     = 200
	listBytes    = 64 << 10  // text cap for one page of a listing
	noteBytes    = 256 << 10 // read_note
	batchBytes   = 512 << 10 // read_notes, all notes together
	maxBatch     = 20
)

// out builds a tool's text output. Text that comes from the vault (note
// content, paths, property values) goes inside one delimited block whose
// nonce is random per call, so a note can't close the block early or
// fake one (X-SRV-07).
type out struct {
	nonce string
	sb    strings.Builder
}

func newOut() *out {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return &out{nonce: hex.EncodeToString(b)}
}

// line writes a line of the server's own text.
func (o *out) line(format string, args ...any) {
	fmt.Fprintf(&o.sb, format, args...)
	o.sb.WriteByte('\n')
}

// untrusted writes vault text in a delimited block. label says what it is.
func (o *out) untrusted(label, text string) {
	for strings.Contains(text, o.nonce) { // never in practice
		o.nonce = newOut().nonce
	}
	fmt.Fprintf(&o.sb, "<<<NOTE-CONTENT nonce=%s %s>>>\n", o.nonce, label)
	o.sb.WriteString(text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		o.sb.WriteByte('\n')
	}
	fmt.Fprintf(&o.sb, "<<<END NOTE-CONTENT nonce=%s>>>\n", o.nonce)
}

func (o *out) String() string { return o.sb.String() }

// PageArgs are the paging arguments of listing tools (X-SRV-06).
type PageArgs struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"the next_cursor of the previous page"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size; default 50, at most 200"`
}

var errBadCursor = errors.New("invalid cursor: pass next_cursor from the previous page unchanged")

// paged is one page of a listing.
type paged[T any] struct {
	items []T
	lines []string // the rendered items
	next  string
}

// page cuts one page from items: at most the limit, and at most listBytes
// of rendered text (X-SRV-06).
func page[T any](items []T, a PageArgs, render func(T) string) (paged[T], error) {
	start := 0
	if a.Cursor != "" {
		var err error
		if start, err = strconv.Atoi(a.Cursor); err != nil || start < 0 {
			return paged[T]{}, errBadCursor
		}
	}
	limit := a.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	var p paged[T]
	size, i := 0, start
	for ; i < len(items) && len(p.lines) < limit; i++ {
		l := render(items[i])
		if size+len(l) > listBytes && len(p.lines) > 0 {
			break
		}
		size += len(l) + 1
		p.lines = append(p.lines, l)
	}
	if start < len(items) {
		p.items = items[start:i]
	}
	if i < len(items) {
		p.next = strconv.Itoa(i)
	}
	return p, nil
}

func ptr[T any](v T) *T { return &v }

// truncate cuts s to at most n bytes, on a line boundary when possible and
// otherwise on a rune boundary.
func truncate(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		return cut[:i+1], true
	}
	for len(cut) > 0 && !utf8.RuneStart(s[len(cut)]) {
		cut = cut[:len(cut)-1]
	}
	return cut, true
}
