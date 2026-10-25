package vault

import (
	"errors"
	"strings"
	"testing"
)

func mustPolicy(t testing.TB, configDir string, deny ...string) *policy {
	t.Helper()
	p, err := newPolicy(configDir, deny)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPolicyHidden(t *testing.T) {
	// V-PATH-07: the deny list applies at every depth and ignores case.
	p := mustPolicy(t, "")
	hidden := []string{
		".obsidian", ".obsidian/app.json", ".OBSIDIAN/app.json", "a/.Obsidian/x.md",
		".git/config", "sub/.GIT/HEAD", ".trash/old.md", "a/.Trash/x.md",
		"node_modules/x/readme.md", "a/Node_Modules/x.md",
	}
	for _, h := range hidden {
		var pe *PathError
		if err := p.check("read", h, accessRead); !errors.As(err, &pe) {
			t.Errorf("V-PATH-07 %q visible; err = %v", h, err)
		}
	}
	for _, ok := range []string{"", "Note.md", "obsidian/x.md", ".obsidian-notes.md", "a/.gitignore.md", "trash/x.md"} {
		if err := p.check("read", ok, accessRead); err != nil {
			t.Errorf("V-PATH-07 %q hidden: %v", ok, err)
		}
	}
}

func TestPolicyConfigDir(t *testing.T) {
	// V-PATH-08: the config folder name can be overridden; it must start with ".".
	p := mustPolicy(t, ".config-obs")
	if err := p.check("read", ".Config-Obs/app.json", accessRead); err == nil {
		t.Error("V-PATH-08 overridden config folder visible")
	}
	if err := p.check("read", ".obsidian/app.json", accessRead); err != nil {
		t.Errorf("V-PATH-08 .obsidian should be an ordinary folder when overridden: %v", err)
	}
	for _, bad := range []string{"config", "a/.b", ".", "..", ".x/"} {
		if _, err := newPolicy(bad, nil); err == nil {
			t.Errorf("V-PATH-08 config folder %q accepted", bad)
		}
	}
}

func TestPolicyDenyGlobs(t *testing.T) {
	// V-PATH-09: OBS_MCP_DENY globs hide paths from every operation.
	if p := mustPolicy(t, ""); len(p.deny) != 0 {
		t.Fatal("V-PATH-09 default deny list not empty")
	}
	p := mustPolicy(t, "", "Journal/**", "Private", "**/*.secret.md", " ", "a/*/c")
	tests := []struct {
		path   string
		denied bool
	}{
		{"Journal", true},
		{"journal/2024/day.md", true},
		{"Private", true},
		{"private/x.md", true},
		{"PrivateNotes/x.md", false},
		{"x.secret.md", true},
		{"deep/er/X.SECRET.md", true},
		{"x.md", false},
		{"a/b/c", true},
		{"a/b/c/d.md", true},
		{"a/b/d.md", false},
	}
	for _, tt := range tests {
		for _, a := range []access{accessRead, accessWrite} {
			err := p.check("op", tt.path, a)
			hid := err != nil && strings.Contains(err.Error(), "deny list")
			if hid != tt.denied {
				t.Errorf("V-PATH-09 %q access %d: err = %v; want denied=%v", tt.path, a, err, tt.denied)
			}
		}
	}
	if _, err := newPolicy("", []string{"a/[x"}); err == nil {
		t.Error("V-PATH-09 bad glob accepted")
	}
}

func TestPolicyWritable(t *testing.T) {
	// V-PATH-10: only .md, .canvas and .base are writable; never the config folder.
	p := mustPolicy(t, "")
	for _, ok := range []string{"a.md", "b/c.canvas", "d.base"} {
		if err := p.check("write", ok, accessWrite); err != nil {
			t.Errorf("V-PATH-10 %q not writable: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a.png", "a.MD", "a.md.exe", "a.js", "a", ".obsidian/x.md", ".obsidian/plugins/p/main.js", ".OBSIDIAN/app.json"} {
		var pe *PathError
		if err := p.check("write", bad, accessWrite); !errors.As(err, &pe) {
			t.Errorf("V-PATH-10 %q writable; err = %v", bad, err)
		}
	}
}

// FuzzPolicy checks V-PATH-12 for the policy: it never panics, and a path it
// lets through never contains a hidden folder.
func FuzzPolicy(f *testing.F) {
	for _, s := range []string{"a.md", ".obsidian/app.json", "x/.GIT/y", "Journal/a.md", "n.canvas"} {
		f.Add(s, "Journal/**")
	}
	f.Fuzz(func(t *testing.T, p, glob string) {
		pol, err := newPolicy("", []string{glob})
		if err != nil {
			return
		}
		rel, err := Clean(p)
		if err != nil {
			return
		}
		for _, a := range []access{accessRead, accessWrite} {
			if pol.check("fuzz", rel, a) != nil {
				continue
			}
			for seg := range strings.SplitSeq(rel, "/") {
				for _, h := range pol.hidden {
					if strings.EqualFold(seg, h) {
						t.Fatalf("V-PATH-12 %q allowed although inside %s", rel, h)
					}
				}
			}
		}
	})
}
