package vault

import "testing"

func TestExcluder(t *testing.T) {
	// V-SRCH-03
	ex := NewExcluder([]string{"pr06/excluded/", "/\\.draft\\.md$/", "/([/", "", "Old"})
	for p, want := range map[string]bool{
		"pr06/excluded/x.md":  true,
		"pr06/excluded2/x.md": false,
		"pr06/x.md":           false,
		"a/b.draft.md":        true,
		"a/b.md":              false,
		"Old notes.md":        true,
		"old.md":              false,
	} {
		if got := ex(p); got != want {
			t.Errorf("V-SRCH-03 excluded(%q) = %v, want %v", p, got, want)
		}
	}
	if NewExcluder(nil)("x.md") {
		t.Error("V-SRCH-03 empty filter excluded a path")
	}
}

func TestMatchGlob(t *testing.T) {
	for _, tt := range []struct {
		pattern, path string
		want          bool
	}{
		{"Projects/**", "projects/a/b.md", true},
		{"Projects/**", "Projects.md", false},
		{"**/*.md", "a/b/c.md", true},
		{"**/*.md", "c.md", true},
		{"*.md", "a/c.md", false},
		{"a/*/c.md", "A/x/C.md", true},
	} {
		got, err := MatchGlob(tt.pattern, tt.path)
		if err != nil || got != tt.want {
			t.Errorf("MatchGlob(%q, %q) = %v, %v", tt.pattern, tt.path, got, err)
		}
	}
	if _, err := MatchGlob("[", "x"); err == nil {
		t.Error("bad glob accepted")
	}
}
