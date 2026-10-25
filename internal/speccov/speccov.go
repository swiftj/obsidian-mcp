// Package speccov checks that every requirement in docs/SPEC_CONTRACT.md is
// named by at least one test once its phase has been reached.
package speccov

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Requirement is one numbered entry of the spec contract.
type Requirement struct {
	ID    string
	Phase int
	Line  int
}

var (
	idPattern = `[A-Z]+(?:-[A-Z]+)+-[0-9]{2}`
	reqLine   = regexp.MustCompile("^- \\*\\*(" + idPattern + ")\\*\\* `p([0-9])` \\S")
	idToken   = regexp.MustCompile(`\b` + idPattern + `\b`)
)

// Parse reads requirements from a spec contract. Every line that starts like a
// requirement ("- **") outside a code fence must be well formed, and IDs must
// be unique.
func Parse(r io.Reader) ([]Requirement, error) {
	var reqs []Requirement
	seen := map[string]int{}
	inFence := false
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(line, "- **") {
			continue
		}
		m := reqLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("line %d: malformed requirement: %q", n, line)
		}
		if prev, dup := seen[m[1]]; dup {
			return nil, fmt.Errorf("line %d: duplicate ID %s (first on line %d)", n, m[1], prev)
		}
		seen[m[1]] = n
		phase, _ := strconv.Atoi(m[2])
		reqs = append(reqs, Requirement{ID: m[1], Phase: phase, Line: n})
	}
	return reqs, sc.Err()
}

// TestIDs returns every requirement-shaped token found in _test.go files under
// root. Directories named in skip are not descended into.
func TestIDs(root string, skip ...string) (map[string]bool, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	fsys := r.FS()
	ids := map[string]bool{}
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if slices.Contains(skip, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		b, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		for _, id := range idToken.FindAllString(string(b), -1) {
			ids[id] = true
		}
		return nil
	})
	return ids, err
}

// Uncovered returns the requirements with Phase <= phase that no test names.
func Uncovered(reqs []Requirement, ids map[string]bool, phase int) []Requirement {
	var out []Requirement
	for _, r := range reqs {
		if r.Phase <= phase && !ids[r.ID] {
			out = append(out, r)
		}
	}
	return out
}
