package vault

import (
	"strings"
	"unicode/utf8"
)

// Clean checks a vault-relative path and returns it unchanged if it is
// acceptable. It never rewrites or decodes a path: anything that would need
// normalising is refused instead. The empty path is refused; use "" only
// where an API documents it as the vault root.
func Clean(p string) (string, error) {
	if reason := badPath(p); reason != "" {
		return "", &PathError{Op: "check", Path: p, Reason: reason}
	}
	return p, nil
}

func badPath(p string) string {
	switch {
	case p == "":
		return "empty path"
	case !utf8.ValidString(p):
		return "invalid UTF-8"
	case p[0] == '/':
		return "absolute path"
	case strings.ContainsRune(p, '\\'):
		return "backslash"
	case hasPercentEscape(p):
		return "percent-encoding"
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return "control character"
		}
	}
	for seg := range strings.SplitSeq(p, "/") {
		switch {
		case seg == "":
			return "empty segment"
		case seg == "." || seg == "..":
			return "dot segment"
		case strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " "):
			return "segment ends in a dot or space"
		}
	}
	return ""
}

func hasPercentEscape(p string) bool {
	for i := 0; i+2 < len(p); i++ {
		if p[i] == '%' && isHex(p[i+1]) && isHex(p[i+2]) {
			return true
		}
	}
	return false
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// badName reports why a new file or folder name can't be used as an
// Obsidian link target (links.md §Invalid characters), or "" if it can.
func badName(seg string) string {
	if i := strings.IndexAny(seg, "#|^:"); i >= 0 {
		return "name contains " + seg[i:i+1]
	}
	for _, s := range []string{"%%", "[[", "]]"} {
		if strings.Contains(seg, s) {
			return "name contains " + s
		}
	}
	return ""
}
