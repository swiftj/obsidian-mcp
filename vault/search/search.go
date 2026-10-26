// Package search is a full-text BM25 index over a vault's notes, with tag,
// property, path and date filters, and tag listings.
package search

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/swiftj/obsidian-mcp/internal/scan"
	"github.com/swiftj/obsidian-mcp/vault"
	"github.com/swiftj/obsidian-mcp/vault/ofm"
	"github.com/swiftj/obsidian-mcp/vault/props"
)

// BM25 parameters.
const (
	k1 = 1.2
	b  = 0.75
)

// Page limits [policy D-010].
const (
	DefaultLimit = 20
	MaxLimit     = 100
	maxSnippets  = 3
	snippetBytes = 160
)

// Options configures an Index. The zero value is valid.
type Options struct {
	Logger *slog.Logger // nil discards debug events
}

// Query is a search. Every set field must match (V-SRCH-02).
type Query struct {
	// Text holds words, all of which must appear, and "quoted phrases",
	// which must appear as written; case is ignored. Empty text matches
	// every note that passes the filters.
	Text string
	// Tags must all be present, ignoring case; a tag also matches its
	// nested tags, so "inbox" matches "inbox/to-read" (V-SRCH-04).
	Tags []string
	// Props maps property names to values. A value matches a scalar or any
	// list item, ignoring case; an empty value only needs the property.
	Props map[string]string
	// Path is a glob (vault.MatchGlob) the note's path must match.
	Path string
	// Since keeps notes modified at or after it.
	Since time.Time
	// Cursor continues a previous page; Limit caps the page size.
	Cursor string
	Limit  int
}

// Result is one matching note.
type Result struct {
	Path     string
	Score    float64 // 0 when the query has no text
	ModTime  time.Time
	Snippets []Snippet
}

// Snippet is a matching line.
type Snippet struct {
	Line int // 1-based
	Text string
}

// Page is one page of results.
type Page struct {
	Results []Result
	Total   int    // matches across all pages
	Next    string // cursor for the next page, or ""
}

// ErrBadCursor is returned for a cursor this index did not hand out.
var ErrBadCursor = errors.New("search: invalid cursor")

type doc struct {
	path   string
	mod    time.Time
	src    []byte
	tf     map[string]int
	length int
	tags   map[string]bool // folded tags, with every parent
	props  *props.Frontmatter
	d      *ofm.Doc
}

// Index is the search index of one vault. It is built on first use and
// brought up to date at the start of every query by comparing modification
// times (V-SRCH-06). It is safe for concurrent use.
type Index struct {
	v  *vault.Vault
	mu sync.Mutex
	sc *scan.Scanner

	excluded func(string) bool
	docs     map[string]*doc
	postings map[string]map[string]int // term → path → term frequency
	totalLen int
}

// New returns an index over v. Nothing is read until the first query.
func New(v *vault.Vault, o Options) *Index {
	return &Index{v: v, sc: scan.New(v, o.Logger), docs: map[string]*doc{}, postings: map[string]map[string]int{}}
}

// refresh brings the index up to date. The caller holds x.mu.
func (x *Index) refresh(ctx context.Context) error {
	cfg, err := x.v.Config(ctx)
	if err != nil {
		return err
	}
	x.excluded = vault.NewExcluder(cfg.UserIgnoreFilters)
	ch, err := x.sc.Refresh(ctx)
	if err != nil {
		return err
	}
	files := x.sc.Files()
	for _, p := range slices.Concat(ch.Removed, ch.Changed) {
		x.remove(p)
	}
	for _, p := range ch.Changed {
		if f := files[p]; f.Doc != nil {
			x.add(f)
		}
	}
	return nil
}

func (x *Index) remove(p string) {
	d := x.docs[p]
	if d == nil {
		return
	}
	for t := range d.tf {
		delete(x.postings[t], p)
		if len(x.postings[t]) == 0 {
			delete(x.postings, t)
		}
	}
	x.totalLen -= d.length
	delete(x.docs, p)
}

// add indexes a note's name, counted twice, and its whole text
// [policy D-010].
func (x *Index) add(f *scan.File) {
	d := &doc{path: f.Path, mod: f.ModTime, src: f.Doc.Src, tf: map[string]int{}, props: f.Props, d: f.Doc}
	name := strings.TrimSuffix(path.Base(f.Path), ".md")
	ts := slices.Concat(tokens(name), tokens(name), tokens(string(f.Doc.Src)))
	for _, t := range ts {
		d.tf[t]++
	}
	d.length = len(ts)
	d.tags = map[string]bool{}
	for _, name := range noteTags(f) {
		for k := range parents(ofm.NormTag(name)) {
			d.tags[k] = true
		}
	}
	for t, n := range d.tf {
		if x.postings[t] == nil {
			x.postings[t] = map[string]int{}
		}
		x.postings[t][f.Path] = n
	}
	x.totalLen += d.length
	x.docs[f.Path] = d
}

// noteTags returns a note's tags as written: inline tags, then the
// frontmatter tags.
func noteTags(f *scan.File) []string {
	var out []string
	for _, t := range f.Doc.Tags {
		out = append(out, t.Name)
	}
	return append(out, f.Props.Tags()...)
}

// parents yields a folded tag and each of its parents: "a/b" gives "a/b"
// and "a".
func parents(tag string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for t := tag; t != ""; {
			if !yield(t) {
				return
			}
			i := strings.LastIndexByte(t, '/')
			if i < 0 {
				return
			}
			t = t[:i]
		}
	}
}

// Search runs q (V-SRCH-01). Results are ranked by BM25 score, then path;
// a query without text lists matches by path. Notes in excluded files are
// left out (V-SRCH-03).
func (x *Index) Search(ctx context.Context, q Query) (*Page, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	offset := 0
	if q.Cursor != "" {
		n, err := strconv.Atoi(q.Cursor)
		if err != nil || n < 0 {
			return nil, ErrBadCursor
		}
		offset = n
	}
	if q.Path != "" {
		if _, err := vault.MatchGlob(q.Path, ""); err != nil {
			return nil, err
		}
	}
	words, phrases := parseQuery(q.Text)
	slices.Sort(words)
	words = slices.Compact(words)

	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.refresh(ctx); err != nil {
		return nil, err
	}
	var res []Result
	for _, p := range x.candidates(words) {
		d := x.docs[p]
		if x.excluded(p) || !x.filter(d, q) || !hasPhrases(d.src, phrases) {
			continue
		}
		res = append(res, Result{Path: p, Score: x.score(d, words), ModTime: d.mod})
	}
	slices.SortFunc(res, func(a, b Result) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), strings.Compare(a.Path, b.Path))
	})
	page := &Page{Total: len(res)}
	if offset >= len(res) {
		return page, nil
	}
	end := min(offset+limit, len(res))
	page.Results = res[offset:end]
	if end < len(res) {
		page.Next = strconv.Itoa(end)
	}
	for i := range page.Results {
		page.Results[i].Snippets = snippets(x.docs[page.Results[i].Path].src, words, phrases)
	}
	return page, nil
}

// candidates returns the notes holding every word, or every note.
func (x *Index) candidates(words []string) []string {
	if len(words) == 0 {
		out := make([]string, 0, len(x.docs))
		for p := range x.docs {
			out = append(out, p)
		}
		return out
	}
	lists := make([]map[string]int, len(words))
	for i, w := range words {
		lists[i] = x.postings[w]
		if len(lists[i]) == 0 {
			return nil
		}
	}
	slices.SortFunc(lists, func(a, b map[string]int) int { return cmp.Compare(len(a), len(b)) })
	var out []string
next:
	for p := range lists[0] {
		for _, l := range lists[1:] {
			if _, ok := l[p]; !ok {
				continue next
			}
		}
		out = append(out, p)
	}
	return out
}

func (x *Index) score(d *doc, words []string) float64 {
	if len(words) == 0 {
		return 0
	}
	n := float64(len(x.docs))
	avg := float64(x.totalLen) / n
	var s float64
	for _, w := range words {
		df := float64(len(x.postings[w]))
		idf := math.Log((n-df+0.5)/(df+0.5) + 1)
		tf := float64(d.tf[w])
		s += idf * tf * (k1 + 1) / (tf + k1*(1-b+b*float64(d.length)/avg))
	}
	return s
}

func (x *Index) filter(d *doc, q Query) bool {
	if !q.Since.IsZero() && d.mod.Before(q.Since) {
		return false
	}
	if q.Path != "" {
		if ok, _ := vault.MatchGlob(q.Path, d.path); !ok {
			return false
		}
	}
	for _, t := range q.Tags {
		if !d.tags[ofm.NormTag(strings.TrimSpace(t))] {
			return false
		}
	}
	for name, want := range q.Props {
		p := d.props.Get(props.Canonical(name))
		if p == nil || (want != "" && !valueMatches(p.Value, want)) {
			return false
		}
	}
	return true
}

func valueMatches(v any, want string) bool {
	if l, ok := v.([]any); ok {
		return slices.ContainsFunc(l, func(e any) bool { return valueMatches(e, want) })
	}
	if v == nil {
		return false
	}
	return strings.EqualFold(fmt.Sprint(v), want)
}

func hasPhrases(src []byte, phrases []string) bool {
	if len(phrases) == 0 {
		return true
	}
	low := bytes.ToLower(src)
	for _, ph := range phrases {
		if !bytes.Contains(low, []byte(ph)) {
			return false
		}
	}
	return true
}

// snippets returns up to maxSnippets lines holding a query word or phrase,
// each cut to about snippetBytes around the first match.
func snippets(src []byte, words, phrases []string) []Snippet {
	if len(words) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, w := range words {
		want[w] = true
	}
	var out []Snippet
	line := 0
	for l := range bytes.Lines(src) {
		line++
		text := strings.TrimSpace(string(l))
		low := strings.ToLower(text)
		at := -1
		for _, ph := range phrases {
			if i := strings.Index(low, ph); i >= 0 && (at < 0 || i < at) {
				at = i
			}
		}
		if at < 0 {
			for _, t := range tokens(text) {
				if want[t] {
					at = max(strings.Index(low, t), 0)
					break
				}
			}
		}
		if at < 0 {
			continue
		}
		if len(low) != len(text) {
			at = 0 // lower-casing changed the byte length
		}
		out = append(out, Snippet{Line: line, Text: cut(text, at)})
		if len(out) == maxSnippets {
			break
		}
	}
	return out
}

// cut trims text to about snippetBytes, starting a little before at, on
// rune boundaries.
func cut(text string, at int) string {
	if len(text) <= snippetBytes {
		return text
	}
	start := max(0, min(at-snippetBytes/4, len(text)-snippetBytes))
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	end := min(len(text), start+snippetBytes)
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	s := text[start:end]
	if start > 0 {
		s = "…" + s
	}
	if end < len(text) {
		s += "…"
	}
	return s
}
