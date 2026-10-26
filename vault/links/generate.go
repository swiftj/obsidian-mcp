package links

import (
	"path"
	"strings"
)

// Style is how a generated link is written.
type Style int

const (
	Wikilink Style = iota // [[target]]: ".md" dropped
	Markdown              // [text](target): ".md" kept, spaces encoded
	Canvas                // a canvas file node: always the full vault path
)

// linkTarget writes the target of a link from the note at from to the file
// to, in the vault's newLinkFormat (L-REN-02, D-006):
//
//   - shortest: the bare name if no other file has it, else the full path
//   - relative: the path from from's folder ("../a/Tie")
//   - absolute: the full vault path
//
// Wikilinks drop ".md" and keep other extensions; Markdown links keep the
// extension and encode only spaces, as "%20".
func (n *names) linkTarget(from, to, format string, style Style) string {
	if style == Canvas {
		return to
	}
	var t string
	switch format {
	case "relative":
		t = relPath(dir(from), to)
	case "absolute":
		t = to
	default: // shortest
		t = to
		if len(n.byBase[strings.ToLower(path.Base(to))]) == 1 {
			t = path.Base(to)
		}
	}
	if style == Markdown {
		return strings.ReplaceAll(t, " ", "%20")
	}
	if strings.EqualFold(path.Ext(t), ".md") {
		t = t[:len(t)-3]
	}
	return t
}

func dir(p string) string {
	d := path.Dir(p)
	if d == "." {
		return ""
	}
	return d
}

// relPath is the path to file to from folder dir, both vault-relative.
func relPath(dir, to string) string {
	var from []string
	if dir != "" {
		from = strings.Split(dir, "/")
	}
	tos := strings.Split(to, "/")
	i := 0
	for i < len(from) && i < len(tos)-1 && from[i] == tos[i] {
		i++
	}
	ups := len(from) - i
	if ups == 0 {
		return strings.Join(tos[i:], "/")
	}
	return strings.Repeat("../", ups) + strings.Join(tos[i:], "/")
}
