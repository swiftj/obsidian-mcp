package search

import (
	"context"
	"slices"
	"strings"

	"github.com/swiftj/obsidian-mcp/vault/ofm"
)

// Tag is one tag of the vault with the number of notes that have it or one
// of its nested tags.
type Tag struct {
	Name   string // the casing of its first occurrence in vault path order
	Count  int
	Parent string // the parent tag's Name, or "" at the top level
}

// Tags lists the vault's tags, sorted ignoring case, with every parent of a
// nested tag (V-SRCH-05). Tags are case-folded: "Inbox" and "inbox" are one
// tag. Notes in excluded files are left out (V-SRCH-03).
func (x *Index) Tags(ctx context.Context) ([]Tag, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.refresh(ctx); err != nil {
		return nil, err
	}
	display := map[string]string{}
	count := map[string]int{}
	paths := make([]string, 0, len(x.docs))
	for p := range x.docs {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		if x.excluded(p) {
			continue
		}
		f := x.sc.Files()[p]
		for _, name := range noteTags(f) {
			written := strings.TrimRight(strings.TrimPrefix(name, "#"), "/")
			for k := range parents(ofm.NormTag(name)) {
				if _, ok := display[k]; !ok {
					display[k] = k
					if len(k) <= len(written) && strings.EqualFold(written[:len(k)], k) {
						display[k] = written[:len(k)]
					}
				}
			}
		}
		for k := range x.docs[p].tags {
			count[k]++
		}
	}
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]Tag, 0, len(keys))
	for _, k := range keys {
		t := Tag{Name: display[k], Count: count[k]}
		if i := strings.LastIndexByte(k, '/'); i >= 0 {
			t.Parent = display[k[:i]]
		}
		out = append(out, t)
	}
	return out, nil
}
