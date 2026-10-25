package udiff

import (
	"math/rand/v2"
	"strings"
	"testing"
)

func TestUnified(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		want     string
	}{
		{"equal", "a\nb\n", "a\nb\n", ""},
		{"new file", "", "x\ny\n", "--- a/f.md\n+++ b/f.md\n@@ -0,0 +1,2 @@\n+x\n+y\n"},
		{"change middle", "1\n2\n3\n4\n5\n6\n7\n8\n9\n", "1\n2\n3\n4\nFIVE\n6\n7\n8\n9\n",
			"--- a/f.md\n+++ b/f.md\n@@ -2,7 +2,7 @@\n 2\n 3\n 4\n-5\n+FIVE\n 6\n 7\n 8\n"},
		{"no final newline", "a\nb", "a\nc",
			"--- a/f.md\n+++ b/f.md\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n"},
		{"two hunks", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n", "X\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\nY\n",
			"--- a/f.md\n+++ b/f.md\n@@ -1,4 +1,4 @@\n-1\n+X\n 2\n 3\n 4\n@@ -9,4 +9,4 @@\n 9\n 10\n 11\n-12\n+Y\n"},
		{"delete all", "a\n", "", "--- a/f.md\n+++ b/f.md\n@@ -1 +0,0 @@\n-a\n"},
	}
	for _, tt := range tests {
		got := Unified("f.md", []byte(tt.old), []byte(tt.new))
		if got != tt.want {
			t.Errorf("%s:\ngot:\n%s\nwant:\n%s", tt.name, got, tt.want)
		}
	}
	if got := Unified("n.md", nil, []byte("x\n")); !strings.HasPrefix(got, "--- /dev/null\n") {
		t.Errorf("nil old should diff from /dev/null:\n%s", got)
	}
}

// TestDiffRoundTrip checks that the edit script rebuilds both inputs.
func TestDiffRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	gen := func() []string {
		out := make([]string, r.IntN(30))
		for i := range out {
			out[i] = string(rune('a'+r.IntN(4))) + "\n"
		}
		return out
	}
	for range 2000 {
		a, b := gen(), gen()
		var ga, gb []string
		for _, o := range diff(a, b) {
			switch o.kind {
			case opEq:
				ga, gb = append(ga, a[o.a]), append(gb, b[o.b])
			case opDel:
				ga = append(ga, a[o.a])
			case opIns:
				gb = append(gb, b[o.b])
			}
		}
		if strings.Join(ga, "") != strings.Join(a, "") || strings.Join(gb, "") != strings.Join(b, "") {
			t.Fatalf("round trip failed:\na=%q\nb=%q", a, b)
		}
	}
}
