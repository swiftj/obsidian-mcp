package links

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/swiftj/obsidian-mcp/internal/scan"
	"github.com/swiftj/obsidian-mcp/vault"
	"github.com/swiftj/obsidian-mcp/vault/ofm"
)

// PlanHeadingRename plans renaming a heading of the note at p and updating
// every link to it: "#Heading" subpaths, including chains through it
// ("#Heading#Sub"), and the note's own "[[#Heading]]" links (L-REN-08).
// heading is a heading path as read_note takes it. Only the heading's text
// changes in the note; its "#" markers stay. etag is the note's etag.
func (x *Index) PlanHeadingRename(ctx context.Context, p, etag, heading, newText string) (*Plan, error) {
	newText = strings.TrimSpace(newText)
	if newText == "" || strings.ContainsAny(newText, "#|\n\r") || strings.Contains(newText, "[[") || strings.Contains(newText, "]]") || strings.HasPrefix(newText, "^") {
		return nil, &vault.PathError{Op: "rename_heading", Path: p, Reason: "the new heading can't be empty or hold #, |, [[, ]], a line break, or start with ^"}
	}
	return x.planContentRename(ctx, p, etag, func(src []byte) ([]edit, func(string) (string, bool), error) {
		span, err := vault.FindSection(src, heading)
		if err != nil {
			return nil, nil, err
		}
		d := ofm.Parse(src)
		k := slices.IndexFunc(d.Headings, func(h ofm.Heading) bool { return h.Start == span.Start })
		if k < 0 {
			return nil, nil, fmt.Errorf("links: heading %q not found", heading)
		}
		h := d.Headings[k]
		line := string(src[h.Start:h.End])
		skip := len(line) - len(strings.TrimLeft(line, " #"))
		i := strings.Index(line[skip:], h.Text)
		if i < 0 || h.Text == "" {
			return nil, nil, fmt.Errorf("links: heading %q has no text to rename", heading)
		}
		at := h.Start + skip + i
		noteEdit := []edit{{at, at + len(h.Text), newText}}
		return noteEdit, func(sub string) (string, bool) {
			if strings.HasPrefix(sub, "#^") {
				return "", false
			}
			return renameInChain(d, k, sub, newText)
		}, nil
	})
}

// renameInChain rewrites the part of subpath sub that names heading k of d.
// The chain resolves like a heading path: the last part is the heading, and
// earlier parts name enclosing headings in order. Exact text is tried
// first, then case-insensitive; with several matches, the first in the
// note wins, as Obsidian opens the first (D-012).
func renameInChain(d *ofm.Doc, k int, sub, newText string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(sub, "#"), "#")
	if len(parts) == 0 || strings.TrimSpace(parts[len(parts)-1]) == "" {
		return "", false
	}
	for _, fold := range []bool{false, true} {
		eq := func(a, b string) bool {
			return a == strings.TrimSpace(b) || (fold && strings.EqualFold(a, strings.TrimSpace(b)))
		}
		for t, h := range d.Headings {
			if !eq(h.Text, parts[len(parts)-1]) {
				continue
			}
			who := make([]int, len(parts)) // the heading each part names
			who[len(parts)-1] = t
			i := 0
			for _, j := range ancestorsOf(d.Headings, t) {
				if i < len(parts)-1 && eq(d.Headings[j].Text, parts[i]) {
					who[i] = j
					i++
				}
			}
			if i != len(parts)-1 {
				continue
			}
			changed := false
			for n, w := range who {
				if w == k {
					parts[n] = newText
					changed = true
				}
			}
			return "#" + strings.Join(parts, "#"), changed
		}
	}
	return "", false
}

func ancestorsOf(hs []ofm.Heading, k int) []int {
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

// PlanBlockRename plans renaming the block ID ^id of the note at p to
// ^newID and updating every "#^id" link to it (L-REN-09). etag is the
// note's etag.
func (x *Index) PlanBlockRename(ctx context.Context, p, etag, id, newID string) (*Plan, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "^")
	newID = strings.TrimPrefix(strings.TrimSpace(newID), "^")
	if !blockIDRE.MatchString(newID) {
		return nil, &vault.PathError{Op: "rename_block", Path: p, Reason: "a block ID is letters, digits and - only"}
	}
	return x.planContentRename(ctx, p, etag, func(src []byte) ([]edit, func(string) (string, bool), error) {
		d := ofm.Parse(src)
		var hit []ofm.BlockID
		for _, b := range d.BlockIDs {
			if strings.EqualFold(b.ID, id) {
				hit = append(hit, b)
			} else if strings.EqualFold(b.ID, newID) {
				return nil, nil, &vault.PathError{Op: "rename_block", Path: p, Reason: "the note already has ^" + b.ID}
			}
		}
		if _, err := vault.FindBlock(src, id); err != nil || len(hit) != 1 {
			return nil, nil, err
		}
		noteEdit := []edit{{hit[0].Start, hit[0].End, "^" + newID}}
		return noteEdit, func(sub string) (string, bool) {
			if strings.EqualFold(sub, "#^"+id) {
				return "#^" + newID, true
			}
			return "", false
		}, nil
	})
}

// planContentRename plans a change to the note at p (its edits from plan)
// plus the links to it whose subpath rename rewrites.
func (x *Index) planContentRename(ctx context.Context, p, etag string, plan func(src []byte) ([]edit, func(string) (string, bool), error)) (*Plan, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.refresh(ctx); err != nil {
		return nil, err
	}
	note, err := x.fileLocked(p)
	if err != nil {
		return nil, err
	}
	f, err := x.v.Read(ctx, note)
	if err != nil {
		return nil, err
	}
	if f.Kind != vault.KindMarkdown {
		return nil, &vault.PathError{Op: "rename", Path: note, Reason: "only notes (.md) have headings and block IDs"}
	}
	if etag == "" {
		return nil, vault.ErrETagRequired
	}
	if etag != f.ETag {
		return nil, &vault.ConflictError{Path: note, Expected: etag, Current: f.ETag}
	}
	noteEdits, rename, err := plan(f.Data)
	if err != nil {
		return nil, err
	}
	sources := map[string]bool{note: true}
	for _, r := range x.in[note] {
		sources[r.Source] = true
	}
	var out Plan
	for _, src := range slices.Sorted(func(yield func(string) bool) {
		for s := range sources {
			if !yield(s) {
				return
			}
		}
	}) {
		cur := f
		if src != note {
			if cur, err = x.v.Read(ctx, src); err != nil {
				return nil, err
			}
		}
		var edits []fileEdit
		for _, r := range extract(scan.Parse(src, cur.Data)) {
			if got, _ := x.names.resolve(src, r.Link.Target); got != note || r.Link.Subpath == "" {
				continue
			}
			if s, ok := rename(r.Link.Subpath); ok {
				edits = append(edits, fileEdit{ref: r, subpath: &s})
			}
		}
		var extra []edit
		if src == note {
			extra = noteEdits
		}
		data, rewrites, skips, ok := patchFileWith(src, cur.Data, edits, extra)
		if src == note && !ok {
			return nil, fmt.Errorf("links: renaming in %s would change how its links parse; edit it with edit_note instead", note)
		}
		out.Rewrites = append(out.Rewrites, rewrites...)
		out.Skipped = append(out.Skipped, skips...)
		if string(data) != string(cur.Data) {
			out.Changes = append(out.Changes, vault.Change{Kind: vault.ChangeWrite, Path: src, ETag: cur.ETag, Data: data})
		}
	}
	return &out, nil
}
