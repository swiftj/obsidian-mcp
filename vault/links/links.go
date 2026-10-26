// Package links indexes the links and embeds of a vault and resolves them the
// way Obsidian does: backlinks, outgoing links, unresolved links and orphans.
package links

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/swiftj/obsidian-mcp/internal/scan"
	"github.com/swiftj/obsidian-mcp/vault"
	"github.com/swiftj/obsidian-mcp/vault/ofm"
)

// Kind says where a link was found.
type Kind string

const (
	KindBody        Kind = "body"        // the note's text
	KindFrontmatter Kind = "frontmatter" // a quoted property value
	KindCanvas      Kind = "canvas"      // a canvas file node
)

// Ref is one link or embed (L-IDX-01).
type Ref struct {
	Source   string
	Line     int // 1-based; 0 for a canvas node
	Kind     Kind
	Property string // KindFrontmatter: the key, plus ".N" for a list item
	Node     string // KindCanvas: the node id
	// Link is the parsed link. For a frontmatter link its spans are offsets
	// into the property value; a canvas node has only Target and Subpath.
	Link ofm.Link
	// Resolved is the vault path the link points at, or "". When the target
	// is ambiguous, Resolved is "" and Candidates lists the matches.
	Resolved   string
	Candidates []string
}

// Unresolved reports whether the link points at no file.
func (r *Ref) Unresolved() bool { return r.Resolved == "" && len(r.Candidates) == 0 }

// Options configures an Index. The zero value is valid.
type Options struct {
	Logger *slog.Logger // nil discards debug events
}

// Index is the link index of one vault. It is built on first use and
// brought up to date at the start of every query by comparing modification
// times (V-SRCH-06). It is safe for concurrent use.
type Index struct {
	v  *vault.Vault
	mu sync.Mutex
	sc *scan.Scanner

	excluded func(string) bool
	names    *names
	out      map[string][]Ref // by source
	in       map[string][]Ref // by resolved target, without self-links
}

// New returns an index over v. Nothing is read until the first query.
func New(v *vault.Vault, o Options) *Index {
	return &Index{v: v, sc: scan.New(v, o.Logger), out: map[string][]Ref{}}
}

// refresh brings the index up to date. The caller holds x.mu.
func (x *Index) refresh(ctx context.Context) error {
	cfg, err := x.v.Config(ctx)
	if err != nil {
		return err
	}
	x.excluded = vault.NewExcluder(cfg.UserIgnoreFilters)
	before := make(map[string]bool, len(x.out))
	for p := range x.sc.Files() {
		before[p] = true
	}
	ch, err := x.sc.Refresh(ctx)
	if err != nil {
		return err
	}
	if ch.Empty() && x.names != nil {
		return nil
	}
	files := x.sc.Files()
	for _, p := range ch.Removed {
		delete(x.out, p)
	}
	for _, p := range ch.Changed {
		x.out[p] = extract(files[p])
	}
	resolveAll := x.names == nil || ch.SetChanged(before)
	if resolveAll {
		x.names = newNames(x.sc.Paths())
	}
	for src, refs := range x.out {
		if resolveAll || slices.Contains(ch.Changed, src) {
			for i := range refs {
				r := &refs[i]
				r.Resolved, r.Candidates = x.names.resolve(src, r.Link.Target)
			}
		}
	}
	x.in = map[string][]Ref{}
	for _, src := range sortedKeys(x.out) {
		for _, r := range x.out[src] {
			if r.Resolved != "" && r.Resolved != src {
				x.in[r.Resolved] = append(x.in[r.Resolved], r)
			}
		}
	}
	return nil
}

// extract lists the links of one file (L-IDX-01): body links and embeds,
// quoted frontmatter links, and canvas file nodes. External links are not
// vault links. A ".base" embed is an ordinary embed.
func extract(f *scan.File) []Ref {
	var refs []Ref
	if f.Doc != nil {
		for _, l := range f.Props.Links {
			refs = append(refs, Ref{Source: f.Path, Line: l.Line, Kind: KindFrontmatter, Property: l.Path, Link: l.Link})
		}
		starts := lineStarts(f.Doc.Src)
		for _, l := range f.Doc.Links {
			if !l.External {
				refs = append(refs, Ref{Source: f.Path, Line: lineAt(starts, l.Start), Kind: KindBody, Link: l})
			}
		}
	}
	for _, n := range f.Canvas {
		refs = append(refs, Ref{Source: f.Path, Kind: KindCanvas, Node: n.ID, Link: ofm.Link{Target: n.File, Subpath: n.Subpath}})
	}
	return refs
}

// Resolve resolves link as if written in the note at from (L-RES-06). link
// is a link as written ("[[Note#H|x]]", "[x](Note.md)") or a bare target
// ("Note#H"). It returns the resolved path, "" when nothing matches, or an
// *AmbiguousError with the candidates.
func (x *Index) Resolve(ctx context.Context, from, link string) (string, error) {
	t := parseTarget(link)
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.refresh(ctx); err != nil {
		return "", err
	}
	p, cands := x.names.resolve(from, t)
	if len(cands) > 0 {
		return "", &AmbiguousError{Target: t, Candidates: cands}
	}
	return p, nil
}

// parseTarget returns the target of a link as written, or of a bare target.
func parseTarget(link string) string {
	s := strings.TrimSpace(link)
	if d := ofm.Parse([]byte(s)); len(d.Links) == 1 && d.Links[0].Start == 0 && d.Links[0].End == len(s) {
		return d.Links[0].Target
	}
	if d := ofm.Parse([]byte("[[" + s + "]]")); len(d.Links) == 1 {
		return d.Links[0].Target
	}
	return s
}

// Outgoing returns the links in the file at p, in line order.
func (x *Index) Outgoing(ctx context.Context, p string) ([]Ref, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	src, err := x.file(ctx, p)
	if err != nil {
		return nil, err
	}
	return cloneRefs(x.out[src]), nil
}

// Backlinks returns the links from other files that resolve to p, sorted by
// source and line (L-BACK-01). Links from excluded files count (V-SRCH-03).
func (x *Index) Backlinks(ctx context.Context, p string) ([]Ref, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	dst, err := x.file(ctx, p)
	if err != nil {
		return nil, err
	}
	return cloneRefs(x.in[dst]), nil
}

// Unresolved returns the links that point at no file, sorted by source and
// line (L-UNRES-01). Ambiguous links are not unresolved. Links in excluded
// files are left out (V-SRCH-03).
func (x *Index) Unresolved(ctx context.Context) ([]Ref, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.refresh(ctx); err != nil {
		return nil, err
	}
	var out []Ref
	for _, src := range sortedKeys(x.out) {
		if x.excluded(src) {
			continue
		}
		for _, r := range x.out[src] {
			if r.Unresolved() {
				out = append(out, r)
			}
		}
	}
	return cloneRefs(out), nil
}

// Orphans returns the files that no other file links to, sorted
// (L-ORPH-01). Excluded files are left out (V-SRCH-03).
func (x *Index) Orphans(ctx context.Context) ([]string, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.refresh(ctx); err != nil {
		return nil, err
	}
	var out []string
	for _, p := range x.sc.Paths() {
		if len(x.in[p]) == 0 && !x.excluded(p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// file refreshes the index and returns the indexed path of p, matched
// ignoring case. The caller holds x.mu.
func (x *Index) file(ctx context.Context, p string) (string, error) {
	c, err := vault.Clean(p)
	if err != nil {
		return "", err
	}
	if err := x.refresh(ctx); err != nil {
		return "", err
	}
	if _, ok := x.sc.Files()[c]; ok {
		return c, nil
	}
	switch ps := x.names.byPath[strings.ToLower(c)]; len(ps) {
	case 0:
		return "", &vault.NotFoundError{Path: c}
	case 1:
		return ps[0], nil
	default:
		return "", &AmbiguousError{Target: c, Candidates: ps}
	}
}

func cloneRefs(refs []Ref) []Ref {
	out := slices.Clone(refs)
	for i := range out {
		out[i].Candidates = slices.Clone(out[i].Candidates)
	}
	return out
}

func sortedKeys(m map[string][]Ref) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i := 0; ; {
		k := bytes.IndexByte(src[i:], '\n')
		if k < 0 {
			return starts
		}
		i += k + 1
		starts = append(starts, i)
	}
}

// lineAt returns the 1-based line holding byte offset off.
func lineAt(starts []int, off int) int {
	n, found := slices.BinarySearch(starts, off)
	if found {
		return n + 1
	}
	return n
}
