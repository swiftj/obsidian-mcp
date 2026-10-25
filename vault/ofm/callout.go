package ofm

import (
	"bytes"
	"strings"
)

// calloutTypes maps every built-in callout type and alias to its canonical
// type (callouts.md §Supported types).
var calloutTypes = map[string]string{
	"note": "note", "abstract": "abstract", "summary": "abstract", "tldr": "abstract",
	"info": "info", "todo": "todo", "tip": "tip", "hint": "tip", "important": "tip",
	"success": "success", "check": "success", "done": "success",
	"question": "question", "help": "question", "faq": "question",
	"warning": "warning", "caution": "warning", "attention": "warning",
	"failure": "failure", "fail": "failure", "missing": "failure",
	"danger": "danger", "error": "danger", "bug": "bug", "example": "example",
	"quote": "quote", "cite": "quote",
}

// parseCallout recognises "[!type]", an optional "+" or "-", and a title on
// the first line of a quote (O-CALL-01).
func parseCallout(t []byte) (Callout, bool) {
	if !bytes.HasPrefix(t, []byte("[!")) {
		return Callout{}, false
	}
	k := bytes.IndexByte(t, ']')
	if k <= 2 || bytes.ContainsAny(t[2:k], " \t[") {
		return Callout{}, false
	}
	c := Callout{Type: strings.ToLower(string(t[2:k]))}
	c.Canon = calloutTypes[c.Type]
	if c.Canon == "" {
		c.Canon = "note"
	}
	rest := t[k+1:]
	if len(rest) > 0 && (rest[0] == '+' || rest[0] == '-') {
		c.Fold, rest = rest[0], rest[1:]
	}
	c.Title = string(bytes.TrimSpace(rest))
	return c, true
}
