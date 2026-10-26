package links

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/swiftj/obsidian-mcp/internal/scan"
	"github.com/swiftj/obsidian-mcp/vault"
	"github.com/swiftj/obsidian-mcp/vault/ofm"
)

// Plan is a planned multi-file change: the changes to apply with
// vault.Apply, and a report of what they do to links.
type Plan struct {
	Changes  []vault.Change
	Rewrites []Rewrite
	Skipped  []Skip
}

// Rewrite is one link the plan changes (L-REN-05).
type Rewrite struct {
	Source string // the file holding the link, at its current path
	Line   int    // 1-based; 0 for a canvas node
	Old    string
	New    string
}

// Skip is a link the plan leaves alone, and why (L-REN-04).
type Skip struct {
	Source string
	Line   int
	Link   string
	Reason string
}

// edit replaces src[start:end] with text.
type edit struct {
	start, end int
	text       string
}

// fileEdit is a planned change to one link of a file.
type fileEdit struct {
	ref     Ref
	target  *string // the new target, as written in the link
	subpath *string // the new subpath, with its leading "#"
}

// PlanMoves plans moving files (old path → new path) and rewriting every
// link whose resolution the moves would change (L-REN-01, L-REN-10). Each
// link that resolved before is resolved again, as written, from its
// source's new path against the file set after the moves; if it would no
// longer reach the moved-to target, it is rewritten in its own style with
// the vault's newLinkFormat. Subpaths, display text and embeds are kept. A
// link whose new form would be ambiguous is reported and left alone
// (L-REN-04).
func (x *Index) PlanMoves(ctx context.Context, moves map[string]string) (*Plan, error) {
	cfg, err := x.v.Config(ctx)
	if err != nil {
		return nil, err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.refresh(ctx); err != nil {
		return nil, err
	}
	files := x.sc.Files()
	mv := map[string]string{}
	taken := map[string]bool{}
	for p := range files {
		taken[strings.ToLower(p)] = true
	}
	for from, to := range moves {
		src, err := x.fileLocked(from)
		if err != nil {
			return nil, err
		}
		dst, err := vault.Clean(to)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(path.Ext(src), path.Ext(dst)) {
			return nil, &vault.PathError{Op: "move", Path: dst, Reason: "a move can't change the file's extension"}
		}
		mv[src] = dst
	}
	for src, dst := range mv {
		if taken[strings.ToLower(dst)] {
			if _, moving := mv[existingPath(files, dst)]; !moving || strings.EqualFold(src, dst) {
				return nil, &vault.ConflictError{Path: dst, Current: "exists"}
			}
		}
	}
	mapped := func(p string) string {
		if q, ok := mv[p]; ok {
			return q
		}
		return p
	}
	var after []string
	seen := map[string]bool{}
	for _, p := range x.sc.Paths() {
		q := mapped(p)
		if seen[strings.ToLower(q)] {
			return nil, &vault.ConflictError{Path: q, Current: "exists"}
		}
		seen[strings.ToLower(q)] = true
		after = append(after, q)
	}
	newNames := newNames(after)
	check := func(src string, r Ref) bool { // true: the link needs a rewrite
		if r.Resolved == "" || len(r.Candidates) > 0 {
			return false
		}
		got, cands := newNames.resolve(mapped(src), r.Link.Target)
		return got != mapped(r.Resolved) || len(cands) > 0
	}
	var plan Plan
	sources := map[string]bool{}
	for src, refs := range x.out {
		for _, r := range refs {
			if check(src, r) {
				sources[src] = true
				break
			}
		}
	}
	for src := range mv {
		sources[src] = true
	}
	for _, src := range slices.Sorted(func(yield func(string) bool) {
		for s := range sources {
			if !yield(s) {
				return
			}
		}
	}) {
		f, err := x.v.Read(ctx, src)
		if err != nil {
			return nil, err
		}
		var edits []fileEdit
		if f.Kind == vault.KindMarkdown || f.Kind == vault.KindCanvas {
			for _, r := range extract(scan.Parse(src, f.Data)) {
				r.Resolved, r.Candidates = x.names.resolve(src, r.Link.Target)
				if !check(src, r) {
					continue
				}
				want := mapped(r.Resolved)
				t, ok := newTarget(newNames, mapped(src), want, cfg.NewLinkFormat, r)
				if !ok {
					plan.Skipped = append(plan.Skipped, Skip{Source: src, Line: r.Line, Link: linkString(r), Reason: "no link to " + want + " would resolve unambiguously from here"})
					continue
				}
				edits = append(edits, fileEdit{ref: r, target: &t})
			}
		}
		data, rewrites, skips, _ := patchFileWith(f.Path, f.Data, edits, nil)
		plan.Rewrites = append(plan.Rewrites, rewrites...)
		plan.Skipped = append(plan.Skipped, skips...)
		changed := !bytes.Equal(data, f.Data)
		switch to, moving := mv[src]; {
		case moving && changed:
			plan.Changes = append(plan.Changes, vault.Change{Kind: vault.ChangeMove, Path: src, To: to, ETag: f.ETag, Data: data})
		case moving:
			plan.Changes = append(plan.Changes, vault.Change{Kind: vault.ChangeMove, Path: src, To: to, ETag: f.ETag})
		case changed:
			plan.Changes = append(plan.Changes, vault.Change{Kind: vault.ChangeWrite, Path: src, ETag: f.ETag, Data: data})
		}
	}
	return &plan, nil
}

// PlanFolderMove plans moving every visible file under folder from to
// folder to, with PlanMoves (L-REN-10). to must not exist.
func (x *Index) PlanFolderMove(ctx context.Context, from, to string) (*Plan, error) {
	src, err := vault.Clean(from)
	if err != nil {
		return nil, err
	}
	dst, err := vault.Clean(to)
	if err != nil {
		return nil, err
	}
	if src == "" || dst == "" {
		return nil, &vault.PathError{Op: "move_folder", Path: from, Reason: "the vault root can't be moved"}
	}
	if strings.HasPrefix(strings.ToLower(dst)+"/", strings.ToLower(src)+"/") {
		return nil, &vault.PathError{Op: "move_folder", Path: to, Reason: "a folder can't move into itself"}
	}
	x.mu.Lock()
	if err := x.refresh(ctx); err != nil {
		x.mu.Unlock()
		return nil, err
	}
	moves := map[string]string{}
	for _, p := range x.sc.Paths() {
		if rest, ok := strings.CutPrefix(p, src+"/"); ok {
			moves[p] = dst + "/" + rest
		}
		if strings.EqualFold(p, dst) || strings.HasPrefix(strings.ToLower(p), strings.ToLower(dst)+"/") {
			x.mu.Unlock()
			return nil, &vault.ConflictError{Path: dst, Current: "exists"}
		}
	}
	x.mu.Unlock()
	if len(moves) == 0 {
		return nil, &vault.NotFoundError{Path: src}
	}
	return x.PlanMoves(ctx, moves)
}

// newTarget writes a target for r that resolves from src to want, trying
// the vault's format first, then the full path, then "./"-relative.
func newTarget(n *names, src, want, format string, r Ref) (string, bool) {
	style := Wikilink
	switch {
	case r.Kind == KindCanvas:
		style = Canvas
	case r.Link.Markdown:
		style = Markdown
	}
	cands := []string{n.linkTarget(src, want, format, style), n.linkTarget(src, want, "absolute", style)}
	if rel := n.linkTarget(src, want, "relative", style); !strings.HasPrefix(rel, "../") {
		cands = append(cands, "./"+rel)
	}
	for _, t := range cands {
		check := t
		if style == Markdown {
			check = strings.ReplaceAll(t, "%20", " ")
		}
		if got, c := n.resolve(src, check); got == want && len(c) == 0 {
			return t, true
		}
	}
	return "", false
}

// patchFileWith applies link edits, plus extra byte edits, to a file's
// bytes. Each link edit is placed by its span; frontmatter and canvas links
// are located in the source first. The result is re-parsed to confirm every
// edited link reads back as planned and no other link changed; a file that
// fails the check is left as it was, and ok is false.
func patchFileWith(p string, src []byte, edits []fileEdit, extra []edit) (out []byte, rewrites []Rewrite, skips []Skip, ok bool) {
	if len(edits) == 0 && len(extra) == 0 {
		return src, nil, nil, true
	}
	all := slices.Clone(extra)
	var nodes []canvasNode
	if vault.KindOf(p) == vault.KindCanvas {
		nodes = canvasNodes(src)
	}
	used := map[string]int{} // frontmatter: occurrences claimed per line and text
	for _, fe := range edits {
		r := fe.ref
		var (
			old string
			at  int // where the link starts in src
			es  []edit
			err error
		)
		switch r.Kind {
		case KindBody:
			old, at = string(src[r.Link.Start:r.Link.End]), r.Link.Start
			es = linkEdits(r.Link, fe, 0)
		case KindFrontmatter:
			old, at, es, err = frontmatterEdits(src, r, fe, used)
		case KindCanvas:
			es, err = canvasEdits(nodes, r, fe)
		}
		if err != nil {
			skips = append(skips, Skip{Source: p, Line: r.Line, Link: linkString(r), Reason: err.Error()})
			continue
		}
		all = append(all, es...)
		rw := Rewrite{Source: p, Line: r.Line, Old: old}
		if r.Kind == KindCanvas {
			rw.Old, rw.New = linkString(r), canvasPreview(r, fe)
		} else {
			local := make([]edit, len(es))
			for i, e := range es {
				local[i] = edit{e.start - at, e.end - at, e.text}
			}
			rw.New = string(applyEdits([]byte(old), local))
		}
		rewrites = append(rewrites, rw)
	}
	out = applyEdits(src, all)
	if why := verify(p, src, out, edits); why != "" {
		for _, fe := range edits {
			skips = append(skips, Skip{Source: p, Line: fe.ref.Line, Link: linkString(fe.ref), Reason: why})
		}
		return src, nil, skips, false
	}
	return out, rewrites, skips, true
}

// linkEdits returns the edits to a link's target and subpath; its spans are
// shifted by base.
func linkEdits(l ofm.Link, fe fileEdit, base int) []edit {
	var es []edit
	if fe.target != nil {
		es = append(es, edit{base + l.TargetSpan.Start, base + l.TargetSpan.End, *fe.target})
	}
	if fe.subpath != nil {
		s := *fe.subpath
		if l.Markdown {
			s = strings.ReplaceAll(s, " ", "%20")
		}
		es = append(es, edit{base + l.SubpathSpan.Start, base + l.SubpathSpan.End, s})
	}
	return es
}

// frontmatterEdits finds a frontmatter link's text in the note, from its
// value's line on, and returns the edits for it. The link must appear as
// written in the value (no YAML escapes), and the new text must need none.
func frontmatterEdits(src []byte, r Ref, fe fileEdit, used map[string]int) (string, int, []edit, error) {
	for _, s := range []*string{fe.target, fe.subpath} {
		if s != nil && strings.ContainsAny(*s, "\"'\\\n") {
			return "", 0, nil, fmt.Errorf("the new link would need YAML escaping")
		}
	}
	d := ofm.Parse(src)
	starts := lineStarts(src)
	if d.Frontmatter == nil || r.Line < 1 || r.Line > len(starts) || r.raw == "" {
		return "", 0, nil, fmt.Errorf("the link was not found in the frontmatter")
	}
	body := d.Frontmatter.Body
	from := max(starts[r.Line-1], body.Start)
	key := strconv.Itoa(from) + "\x00" + r.raw
	pos, at := from, -1
	for n := 0; n <= used[key]; n++ {
		i := bytes.Index(src[pos:body.End], []byte(r.raw))
		if i < 0 {
			return "", 0, nil, fmt.Errorf("the link is written with YAML escapes")
		}
		at = pos + i
		pos = at + len(r.raw)
	}
	used[key]++
	return r.raw, at, linkEdits(r.Link, fe, at-r.Link.Start), nil
}

// canvasNode is one node's "file" and "subpath" value positions.
type canvasNode struct {
	id            string
	file, subpath [2]int // byte range of the JSON string token, or {-1,-1}
}

var errNoNode = fmt.Errorf("the canvas node was not found")

// canvasEdits returns the edits for a canvas file node.
func canvasEdits(nodes []canvasNode, r Ref, fe fileEdit) ([]edit, error) {
	for _, n := range nodes {
		if n.id != r.Node {
			continue
		}
		var es []edit
		if fe.target != nil {
			if n.file[0] < 0 {
				return nil, errNoNode
			}
			es = append(es, edit{n.file[0], n.file[1], jsonString(*fe.target)})
		}
		if fe.subpath != nil {
			if n.subpath[0] < 0 {
				return nil, errNoNode
			}
			es = append(es, edit{n.subpath[0], n.subpath[1], jsonString(*fe.subpath)})
		}
		return es, nil
	}
	return nil, errNoNode
}

func jsonString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// canvasNodes walks a canvas's JSON tokens and records where each node's
// "id", "file" and "subpath" strings are.
func canvasNodes(src []byte) []canvasNode {
	dec := json.NewDecoder(bytes.NewReader(src))
	var out []canvasNode
	tok := func() (json.Token, int, bool) {
		t, err := dec.Token()
		return t, int(dec.InputOffset()), err == nil
	}
	skip := func(t json.Token) bool { // skip a value whose first token is t
		d, ok := t.(json.Delim)
		if !ok || (d != '{' && d != '[') {
			return true
		}
		for depth := 1; depth > 0; {
			t, _, ok := tok()
			if !ok {
				return false
			}
			if d, isD := t.(json.Delim); isD {
				if d == '{' || d == '[' {
					depth++
				} else {
					depth--
				}
			}
		}
		return true
	}
	if t, _, ok := tok(); !ok || t != json.Delim('{') {
		return nil
	}
	for dec.More() {
		k, _, ok := tok()
		if !ok {
			return out
		}
		v, _, ok := tok()
		if !ok {
			return out
		}
		if k != "nodes" || v != json.Delim('[') {
			if !skip(v) {
				return out
			}
			continue
		}
		for dec.More() {
			t, _, ok := tok()
			if !ok || t != json.Delim('{') {
				if !ok || !skip(t) {
					return out
				}
				continue
			}
			n := canvasNode{file: [2]int{-1, -1}, subpath: [2]int{-1, -1}}
			for dec.More() {
				k, kend, ok := tok()
				if !ok {
					return out
				}
				v, vend, ok := tok()
				if !ok {
					return out
				}
				s, isStr := v.(string)
				if !isStr {
					if !skip(v) {
						return out
					}
					continue
				}
				q := kend + bytes.IndexByte(src[kend:], ':')
				q += bytes.IndexByte(src[q:], '"')
				switch k {
				case "id":
					n.id = s
				case "file":
					n.file = [2]int{q, vend}
				case "subpath":
					n.subpath = [2]int{q, vend}
				}
			}
			if _, _, ok := tok(); !ok { // "}"
				return out
			}
			out = append(out, n)
		}
		if _, _, ok := tok(); !ok { // "]"
			return out
		}
	}
	return out
}

func applyEdits(src []byte, es []edit) []byte {
	slices.SortFunc(es, func(a, b edit) int { return b.start - a.start })
	out := slices.Clone(src)
	for _, e := range es {
		out = slices.Concat(out[:e.start], []byte(e.text), out[e.end:])
	}
	return out
}

func canvasPreview(r Ref, fe fileEdit) string {
	t, s := r.Link.Target, r.Link.Subpath
	if fe.target != nil {
		t = *fe.target
	}
	if fe.subpath != nil {
		s = *fe.subpath
	}
	return "canvas card " + t + s
}

// verify re-parses the patched file and checks that every link reads back
// as planned and nothing else changed. It returns why not, or "".
func verify(p string, before, after []byte, edits []fileEdit) string {
	old := extract(scan.Parse(p, before))
	now := extract(scan.Parse(p, after))
	if len(old) != len(now) {
		return "rewriting would change how the file's other links parse"
	}
	type key struct {
		kind Kind
		line int
		node string
		prop string
		tgt  string
		sub  string
	}
	planned := map[key]fileEdit{}
	for _, fe := range edits {
		r := fe.ref
		planned[key{r.Kind, r.Line, r.Node, r.Property, r.Link.Target, r.Link.Subpath}] = fe
	}
	for i := range old {
		o, n := old[i], now[i]
		wantT, wantS := o.Link.Target, o.Link.Subpath
		if fe, ok := planned[key{o.Kind, o.Line, o.Node, o.Property, o.Link.Target, o.Link.Subpath}]; ok {
			if fe.target != nil {
				wantT = *fe.target
				if o.Link.Markdown {
					wantT = strings.ReplaceAll(wantT, "%20", " ")
				}
			}
			if fe.subpath != nil {
				wantS = *fe.subpath
			}
		}
		if n.Link.Target != wantT || n.Link.Subpath != wantS || n.Link.Display != o.Link.Display || n.Link.Embed != o.Link.Embed {
			return fmt.Sprintf("the rewritten link would read back as %q", linkString(n))
		}
	}
	return ""
}

// linkString shows a link the way it is written, rebuilt from its parts.
func linkString(r Ref) string {
	l := r.Link
	if r.Kind == KindCanvas {
		return "canvas card " + l.Target + l.Subpath
	}
	bang := ""
	if l.Embed {
		bang = "!"
	}
	if l.Markdown {
		return fmt.Sprintf("%s[%s](%s%s)", bang, l.Display, l.Target, l.Subpath)
	}
	disp := ""
	if l.HasDisplay {
		disp = "|" + l.Display
	}
	return fmt.Sprintf("%s[[%s%s%s]]", bang, l.Target, l.Subpath, disp)
}

// existingPath returns the indexed spelling of p, matched ignoring case.
func existingPath(files map[string]*scan.File, p string) string {
	for q := range files {
		if strings.EqualFold(q, p) {
			return q
		}
	}
	return p
}

var blockIDRE = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
