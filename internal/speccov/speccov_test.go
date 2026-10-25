package speccov

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is the module root, two levels above this package.
var repoRoot = filepath.Join("..", "..")

func readSpec(t *testing.T) []Requirement {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "docs", "SPEC_CONTRACT.md"))
	if err != nil {
		t.Fatal(err)
	}
	reqs, err := Parse(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return reqs
}

func currentPhase(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "docs", "PHASE"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("docs/PHASE: %v", err)
	}
	return p
}

// M-SPEC-01: IDs are well formed, unique and phase-tagged.
func TestSpecWellFormed(t *testing.T) {
	reqs := readSpec(t)
	if len(reqs) == 0 {
		t.Fatal("no requirements parsed")
	}
}

// M-SPEC-02: every requirement up to the current phase is named by a test.
func TestSpecCoverage(t *testing.T) {
	reqs := readSpec(t)
	ids, err := TestIDs(repoRoot, ".git", ".gocache", "dist", "testdata")
	if err != nil {
		t.Fatal(err)
	}
	phase := currentPhase(t)
	for _, r := range Uncovered(reqs, ids, phase) {
		t.Errorf("%s (phase %d, SPEC_CONTRACT.md:%d) has no test naming it", r.ID, r.Phase, r.Line)
	}
}

// M-SRC-01: every pinned source matches its recorded sha256.
func TestSourceChecksums(t *testing.T) {
	docs := filepath.Join(repoRoot, "docs")
	sums, err := os.ReadFile(filepath.Join(docs, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	n := 0
	for sc.Scan() {
		sum, name, ok := strings.Cut(sc.Text(), "  ")
		if !ok {
			t.Fatalf("malformed line %q", sc.Text())
		}
		b, err := os.ReadFile(filepath.Join(docs, filepath.FromSlash(name)))
		if err != nil {
			t.Error(err)
			continue
		}
		got := sha256.Sum256(b)
		if hex.EncodeToString(got[:]) != sum {
			t.Errorf("%s: sha256 mismatch", name)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("SHA256SUMS is empty")
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{"ok", "# T\n- **Z-FAKE-01** `p1` Does a thing.\n- **Z-FAKE-PLUG-02** `p7` Another.\nprose\n", []string{"Z-FAKE-01", "Z-FAKE-PLUG-02"}, false},
		{"fenced example ignored", "```\n- **ID** `pN` example\n```\n- **Z-FAKE-01** `p0` Real.\n", []string{"Z-FAKE-01"}, false},
		{"missing phase", "- **Z-FAKE-01** Does a thing.\n", nil, true},
		{"bad id", "- **ZFAKE1** `p1` Does a thing.\n", nil, true},
		{"duplicate", "- **Z-FAKE-01** `p1` A.\n- **Z-FAKE-01** `p2` B.\n", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqs, err := Parse(strings.NewReader(tt.in))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			var got []string
			for _, r := range reqs {
				got = append(got, r.ID)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUncovered(t *testing.T) {
	reqs := []Requirement{{ID: "Z-FAKE-01", Phase: 0}, {ID: "Z-FAKE-02", Phase: 0}, {ID: "Z-FAKE-03", Phase: 3}}
	got := Uncovered(reqs, map[string]bool{"Z-FAKE-01": true}, 0)
	if len(got) != 1 || got[0].ID != "Z-FAKE-02" {
		t.Errorf("got %v, want only Z-FAKE-02", got)
	}
}
