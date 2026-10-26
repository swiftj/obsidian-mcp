package links

import (
	"path"
	"slices"
	"strings"
)

// AmbiguousError reports a link target that matches more than one file
// equally well (L-RES-02). Obsidian's own choice there is not predictable,
// so it is never guessed.
type AmbiguousError struct {
	Target     string
	Candidates []string // sorted vault paths
}

func (e *AmbiguousError) Error() string {
	return "links: " + e.Target + " is ambiguous: " + strings.Join(e.Candidates, ", ")
}

// names is a case-insensitive lookup of the visible vault files.
type names struct {
	byPath map[string][]string // lower-cased path → paths
	byBase map[string][]string // lower-cased base name, with extension → paths
}

func newNames(paths []string) *names {
	n := &names{byPath: map[string][]string{}, byBase: map[string][]string{}}
	for _, p := range paths {
		lp := strings.ToLower(p)
		n.byPath[lp] = append(n.byPath[lp], p)
		b := path.Base(lp)
		n.byBase[b] = append(n.byBase[b], p)
	}
	return n
}

// exact finds the file at vault path lp (lower-cased), with or without ".md".
func (n *names) exact(lp string) []string {
	if ps := n.byPath[lp+".md"]; len(ps) > 0 {
		return ps
	}
	return n.byPath[lp]
}

// resolve resolves a link target written in the note at from (L-RES-01..05,
// DECISIONS D-010):
//
//  1. "" is the note itself.
//  2. "./", "../" and a leading "/" are paths relative to the note's folder
//     or to the vault root, and nothing else is tried.
//  3. Otherwise, an exact vault path wins, then a path relative to the
//     note's folder.
//  4. Otherwise, every file whose path ends with the target at a folder
//     boundary is a candidate: the one in the note's folder wins, then the
//     shortest path. A tie leaves several candidates.
//
// Matching ignores case and ".md" is optional. Aliases never take part
// (L-RES-04).
func (n *names) resolve(from, target string) (string, []string) {
	t := strings.TrimSpace(target)
	if t == "" {
		return from, nil
	}
	lt := strings.ToLower(t)
	dir := strings.ToLower(path.Dir(from))
	if dir == "." {
		dir = ""
	}
	switch {
	case strings.HasPrefix(lt, "/"):
		return pick(n.exact(strings.TrimLeft(path.Clean(lt), "/")))
	case lt == "." || lt == ".." || strings.HasPrefix(lt, "./") || strings.HasPrefix(lt, "../"):
		p := path.Join(dir, lt)
		if p == ".." || strings.HasPrefix(p, "../") {
			return "", nil
		}
		return pick(n.exact(p))
	}
	lt = strings.TrimSuffix(lt, "/")
	if ps := n.exact(lt); len(ps) > 0 {
		return pick(ps)
	}
	if dir != "" {
		if ps := n.exact(dir + "/" + lt); len(ps) > 0 {
			return pick(ps)
		}
	}
	base := path.Base(lt)
	var cands []string
	for _, p := range slices.Concat(n.byBase[base+".md"], n.byBase[base]) {
		lp := strings.ToLower(p)
		if lp == lt || lp == lt+".md" || strings.HasSuffix(lp, "/"+lt) || strings.HasSuffix(lp, "/"+lt+".md") {
			cands = append(cands, p)
		}
	}
	if len(cands) <= 1 {
		return pick(cands)
	}
	var near []string
	for _, p := range cands {
		if pd := strings.ToLower(path.Dir(p)); pd == dir || (pd == "." && dir == "") {
			near = append(near, p)
		}
	}
	if len(near) > 0 {
		return pick(near)
	}
	short := len(cands[0])
	for _, p := range cands {
		short = min(short, len(p))
	}
	var best []string
	for _, p := range cands {
		if len(p) == short {
			best = append(best, p)
		}
	}
	return pick(best)
}

// pick returns the only path, or every candidate when there are several.
func pick(ps []string) (string, []string) {
	switch len(ps) {
	case 0:
		return "", nil
	case 1:
		return ps[0], nil
	}
	c := slices.Clone(ps)
	slices.Sort(c)
	return "", slices.Compact(c)
}
