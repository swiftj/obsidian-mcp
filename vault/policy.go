package vault

import (
	"fmt"
	"path"
	"strings"
)

// DefaultConfigDir is the name of Obsidian's configuration folder.
const DefaultConfigDir = ".obsidian"

const trashDir = ".trash"

// access is what an operation wants to do with a path.
type access int

const (
	accessRead  access = iota
	accessWrite        // change content: .md, .canvas and .base only
	accessFile         // move a whole file of any visible kind, such as into .trash
)

// policy decides which clean paths an operation may touch.
type policy struct {
	configDir string
	hidden    []string // folder names refused at any depth, compared case-insensitively
	deny      [][]string
}

func newPolicy(configDir string, deny []string) (*policy, error) {
	if configDir == "" {
		configDir = DefaultConfigDir
	}
	if !strings.HasPrefix(configDir, ".") || strings.Contains(configDir, "/") || badPath(configDir) != "" {
		return nil, fmt.Errorf("vault: config folder %q must be a single name starting with \".\"", configDir)
	}
	p := &policy{
		configDir: configDir,
		hidden:    []string{configDir, ".git", trashDir, "node_modules"},
	}
	for _, g := range deny {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		segs := strings.Split(strings.ToLower(strings.Trim(g, "/")), "/")
		for _, s := range segs {
			if _, err := path.Match(s, ""); err != nil {
				return nil, fmt.Errorf("vault: bad deny pattern %q: %w", g, err)
			}
		}
		p.deny = append(p.deny, segs)
	}
	return p, nil
}

// check refuses a clean path that the policy hides from op, or that op may
// not write. The vault root ("") is always readable.
func (p *policy) check(op string, rel string, a access) error {
	if rel == "" {
		if a != accessRead {
			return &PathError{Op: op, Path: rel, Reason: "the vault root is not a file"}
		}
		return nil
	}
	if reason := p.hiddenReason(rel); reason != "" {
		return &PathError{Op: op, Path: rel, Reason: reason}
	}
	if a == accessWrite && !writableExt(rel) {
		return &PathError{Op: op, Path: rel, Reason: "only .md, .canvas and .base files can be written"}
	}
	return nil
}

// hiddenReason reports why rel is invisible, or "" if it is visible.
func (p *policy) hiddenReason(rel string) string {
	for seg := range strings.SplitSeq(rel, "/") {
		for _, h := range p.hidden {
			if strings.EqualFold(seg, h) {
				return "inside " + h
			}
		}
	}
	if p.denied(rel) {
		return "hidden by the deny list"
	}
	return ""
}

// denied reports whether rel, or any folder above it, matches a deny glob.
func (p *policy) denied(rel string) bool {
	if len(p.deny) == 0 {
		return false
	}
	segs := strings.Split(strings.ToLower(rel), "/")
	for _, g := range p.deny {
		for n := 1; n <= len(segs); n++ {
			if globMatch(g, segs[:n]) {
				return true
			}
		}
	}
	return false
}

// globMatch matches path segments against glob segments, where "**" matches
// zero or more whole segments and other segments use path.Match.
func globMatch(g, segs []string) bool {
	for len(g) > 0 {
		if g[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if globMatch(g[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(g[0], segs[0]); !ok {
			return false
		}
		g, segs = g[1:], segs[1:]
	}
	return len(segs) == 0
}

func writableExt(rel string) bool {
	switch path.Ext(rel) {
	case ".md", ".canvas", ".base":
		return true
	}
	return false
}
