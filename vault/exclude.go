package vault

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// NewExcluder returns a test for Obsidian's "Excluded files" setting
// (userIgnoreFilters, V-SRCH-03). An entry wrapped in slashes, "/re/", is a
// regular expression matched anywhere in the path; any other entry is a path
// prefix, such as the folder form "Archive/". Entries that don't compile are
// ignored (DECISIONS D-010).
func NewExcluder(filters []string) func(p string) bool {
	var prefixes []string
	var res []*regexp.Regexp
	for _, f := range filters {
		if len(f) >= 2 && strings.HasPrefix(f, "/") && strings.HasSuffix(f, "/") {
			if re, err := regexp.Compile(f[1 : len(f)-1]); err == nil {
				res = append(res, re)
			}
			continue
		}
		if f != "" {
			prefixes = append(prefixes, f)
		}
	}
	return func(p string) bool {
		for _, f := range prefixes {
			if strings.HasPrefix(p, f) {
				return true
			}
		}
		for _, re := range res {
			if re.MatchString(p) {
				return true
			}
		}
		return false
	}
}

// MatchGlob reports whether the vault path p matches pattern, ignoring case.
// Segments use path.Match syntax, and "**" matches any number of whole
// segments: "Projects/**" matches everything under Projects.
func MatchGlob(pattern, p string) (bool, error) {
	g := strings.Split(strings.ToLower(strings.Trim(pattern, "/")), "/")
	for _, s := range g {
		if _, err := path.Match(s, ""); err != nil {
			return false, fmt.Errorf("vault: bad glob %q: %w", pattern, err)
		}
	}
	return globMatch(g, strings.Split(strings.ToLower(p), "/")), nil
}
