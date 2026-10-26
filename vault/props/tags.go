package props

import (
	"slices"
	"strings"

	"github.com/swiftj/obsidian-mcp/vault/ofm"
)

// ValidTag reports whether name, without "#", reads back as exactly that
// one tag (O-TAG-01).
func ValidTag(name string) bool {
	d := ofm.Parse([]byte("#" + name))
	return len(d.Tags) == 1 && d.Tags[0].Name == name && d.Tags[0].Start == 0
}

// RenameTag renames tag old to new in a note, ignoring case, and returns
// the patched note and the number of tags renamed (P-TAGREN-01). Nested
// tags follow: renaming "a" to "b" turns "a/x" into "b/x", keeping "/x" as
// written. Inline tags are patched in place; the frontmatter tags list is
// rewritten with Set, so only its lines change. Tags in code and comments
// aren't tags, so they stay.
func RenameTag(src []byte, old, new string) ([]byte, int, error) {
	old = ofm.NormTag(old)
	new = strings.Trim(strings.TrimPrefix(strings.TrimSpace(new), "#"), "/")
	if old == "" || !ValidTag(new) {
		return nil, 0, &Error{Name: "tags", Reason: "invalid tag name", Hint: "tags are letters, digits, _, - and / with at least one non-digit and no spaces"}
	}
	renamed := func(name string) (string, bool) {
		bare := strings.TrimPrefix(name, "#")
		n := ofm.NormTag(bare)
		if n != old && !strings.HasPrefix(n, old+"/") {
			return "", false
		}
		if len(bare) < len(old) || !strings.EqualFold(bare[:len(old)], old) {
			return "", false // case folding changed the length: leave it
		}
		return strings.TrimSuffix(name, bare) + new + bare[len(old):], true
	}
	count := 0
	out := src
	// Frontmatter first: Set re-reads the note.
	f := Read(ofm.Parse(out))
	if p := f.Get("tags"); p != nil && p.Key == "tags" {
		var value any
		switch v := p.Value.(type) {
		case string:
			if r, ok := renamed(v); ok {
				value, count = r, count+1
			}
		case []any:
			items := slices.Clone(v)
			changed := false
			for i, e := range items {
				if s, ok := e.(string); ok {
					if r, ok := renamed(s); ok {
						items[i], changed = r, true
						count++
					}
				}
			}
			if changed {
				value = items
			}
		}
		if value != nil {
			var err error
			if out, err = Set(out, "tags", value, SetOptions{Types: Types{"tags": Tags}}); err != nil {
				return nil, 0, err
			}
		}
	}
	d := ofm.Parse(out)
	for i := len(d.Tags) - 1; i >= 0; i-- {
		t := d.Tags[i]
		r, ok := renamed(t.Name)
		if !ok {
			continue
		}
		at := t.Start + 1 // after "#"
		out = slices.Concat(out[:at], []byte(r), out[at+len(t.Name):])
		count++
	}
	return out, count, nil
}
