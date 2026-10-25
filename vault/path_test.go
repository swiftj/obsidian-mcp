package vault

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	tests := []struct {
		id, path string
		ok       bool
	}{
		{"V-PATH-01", "Note.md", true},
		{"V-PATH-01", "Folder/Sub/Note.md", true},
		{"V-PATH-01", "Folder", true},
		{"V-PATH-01", "/etc/passwd", false},
		{"V-PATH-01", "/Note.md", false},
		{"V-PATH-02", "", false},
		{"V-PATH-02", "a//b.md", false},
		{"V-PATH-02", "a/", false},
		{"V-PATH-02", "./a.md", false},
		{"V-PATH-02", "a/./b.md", false},
		{"V-PATH-02", "../a.md", false},
		{"V-PATH-02", "a/../../b.md", false},
		{"V-PATH-02", "..", false},
		{"V-PATH-02", "...md", true},
		{"V-PATH-02", "a..b.md", true},
		{"V-PATH-03", "a\x00.md", false},
		{"V-PATH-03", `a\b.md`, false},
		{"V-PATH-03", `..\..\x.md`, false},
		{"V-PATH-03", "%2e%2e/x.md", false},
		{"V-PATH-03", "a%2Fb.md", false},
		{"V-PATH-03", "100% done.md", true},
		{"V-PATH-03", "50%x.md", true},
		{"V-PATH-03", "a\x01b.md", false},
		{"V-PATH-03", "a\nb.md", false},
		{"V-PATH-03", "bad\xffutf8.md", false},
		{"V-PATH-04", "Note.", false},
		{"V-PATH-04", "Note ", false},
		{"V-PATH-04", "Folder./x.md", false},
		{"V-PATH-04", "Folder /x.md", false},
		{"V-PATH-04", " lead.md", true},
		{"V-PATH-01", "日本語/ノート.md", true},
	}
	for _, tt := range tests {
		got, err := Clean(tt.path)
		if tt.ok {
			if err != nil || got != tt.path {
				t.Errorf("%s Clean(%q) = %q, %v; want unchanged", tt.id, tt.path, got, err)
			}
			continue
		}
		var pe *PathError
		if !errors.As(err, &pe) {
			t.Errorf("%s Clean(%q) err = %v; want *PathError", tt.id, tt.path, err)
		}
	}
}

func TestBadName(t *testing.T) {
	// V-WRITE-08
	for _, n := range []string{"a#b.md", "a|b.md", "a^b.md", "a:b.md", "a%%b.md", "a[[b.md", "a]]b.md"} {
		if badName(n) == "" {
			t.Errorf("V-WRITE-08 badName(%q) accepted", n)
		}
	}
	for _, n := range []string{"Note.md", "a [b] c.md", "50% off.md", "a]b[.md", "émoji🙂.md"} {
		if r := badName(n); r != "" {
			t.Errorf("V-WRITE-08 badName(%q) = %q; want accepted", n, r)
		}
	}
}

// FuzzClean checks V-PATH-12: Clean never panics, and any path it accepts
// stays inside the root when joined, without being changed by cleaning.
func FuzzClean(f *testing.F) {
	for _, s := range []string{"a.md", "../x", "a/../b", "%2e%2e/x", "a\\b", "/abs", "a//b", "a/.", "ok/dir/n.md", ".obsidian/app.json"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, p string) {
		got, err := Clean(p)
		if err != nil {
			return
		}
		const root = "/vault"
		j := filepath.Join(root, got)
		if !strings.HasPrefix(j, root+"/") {
			t.Fatalf("V-PATH-12 %q escapes the root: %q", p, j)
		}
		if j != root+"/"+got {
			t.Fatalf("V-PATH-12 %q changed when joined: %q", p, j)
		}
	})
}
