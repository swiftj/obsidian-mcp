package canvas

import (
	"fmt"
	"regexp"
	"slices"
)

// Problem is one way a canvas breaks JSON Canvas 1.0.
type Problem struct {
	Where string `json:"where"` // `node "abc"`, `edge "e1"`, `nodes[3]` or "canvas"
	Msg   string `json:"msg"`
}

func (p Problem) String() string { return p.Where + ": " + p.Msg }

var (
	intRE   = regexp.MustCompile(`^-?[0-9]+$`)
	colorRE = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

	nodeTypes = []string{"text", "file", "link", "group"}
	sides     = []string{"top", "right", "bottom", "left"}
	ends      = []string{"none", "arrow"}
	bgStyles  = []string{"cover", "ratio", "repeat"}
)

// ValidColor reports whether s is a canvas colour: a preset "1" to "6" or
// a hex colour "#RRGGBB" (C-COL-01, D-013).
func ValidColor(s string) bool {
	return len(s) == 1 && s[0] >= '1' && s[0] <= '6' || colorRE.MatchString(s)
}

// Validate checks the canvas against JSON Canvas 1.0 (C-SCH-01 to
// C-COL-01) and returns its problems in file order.
func (c *Canvas) Validate() []Problem {
	var ps []Problem
	add := func(where, format string, a ...any) {
		ps = append(ps, Problem{where, fmt.Sprintf(format, a...)})
	}
	for _, key := range []string{"nodes", "edges"} {
		if v := c.root.get(key); v != nil && v.kind != kArray {
			add("canvas", "%q must be an array", key)
		}
	}
	ids := map[string]bool{}
	nodeIDs := map[string]bool{}
	where := func(kind string, i int, v *value) string {
		if id := str(v, "id"); id != "" {
			return fmt.Sprintf("%s %q", kind, id)
		}
		return fmt.Sprintf("%ss[%d]", kind, i)
	}
	// optional checks an optional string field against a set of values.
	optional := func(w string, v *value, key string, allowed []string) {
		f := v.get(key)
		switch {
		case f == nil:
		case f.kind != kString:
			add(w, "%q must be a string", key)
		case allowed != nil && !slices.Contains(allowed, f.str):
			add(w, "%q must be one of %v, not %q", key, allowed, f.str)
		}
	}
	required := func(w string, v *value, key string) {
		f := v.get(key)
		switch {
		case f == nil:
			add(w, "%q is required", key)
		case f.kind != kString:
			add(w, "%q must be a string", key)
		}
	}
	color := func(w string, v *value) {
		optional(w, v, "color", nil)
		if f := v.get("color"); f != nil && f.kind == kString && !ValidColor(f.str) {
			add(w, `"color" must be a preset "1" to "6" or a hex colour like "#FF0000", not %q`, f.str)
		}
	}
	id := func(w string, v *value) string {
		f := v.get("id")
		if f == nil || f.kind != kString || f.str == "" {
			add(w, `"id" must be a non-empty string`)
			return ""
		}
		if ids[f.str] {
			add(w, "id %q is used more than once", f.str)
		}
		ids[f.str] = true
		return f.str
	}
	for i, v := range c.array("nodes") {
		w := where("node", i, v)
		if v.kind != kObject {
			add(w, "a node must be an object")
			continue
		}
		if n := id(w, v); n != "" {
			nodeIDs[n] = true
		}
		for _, key := range []string{"x", "y", "width", "height"} {
			f := v.get(key)
			if f == nil || f.kind != kNumber || !intRE.Match(c.src[f.start:f.end]) {
				add(w, "%q must be an integer", key)
			}
		}
		color(w, v)
		switch t := v.get("type"); {
		case t == nil || t.kind != kString:
			add(w, `"type" must be one of %v`, nodeTypes)
		case t.str == "text":
			required(w, v, "text")
		case t.str == "file":
			required(w, v, "file")
			optional(w, v, "subpath", nil)
			if s := v.get("subpath"); s != nil && s.kind == kString && (s.str == "" || s.str[0] != '#') {
				add(w, `"subpath" must start with "#"`)
			}
		case t.str == "link":
			required(w, v, "url")
		case t.str == "group":
			optional(w, v, "label", nil)
			optional(w, v, "background", nil)
			optional(w, v, "backgroundStyle", bgStyles)
		default:
			add(w, `"type" must be one of %v, not %q`, nodeTypes, t.str)
		}
	}
	for i, v := range c.array("edges") {
		w := where("edge", i, v)
		if v.kind != kObject {
			add(w, "an edge must be an object")
			continue
		}
		id(w, v)
		for _, key := range []string{"fromNode", "toNode"} {
			required(w, v, key)
			if f := v.get(key); f != nil && f.kind == kString && !nodeIDs[f.str] {
				add(w, "%q names no node: %q", key, f.str)
			}
		}
		optional(w, v, "fromSide", sides)
		optional(w, v, "toSide", sides)
		optional(w, v, "fromEnd", ends)
		optional(w, v, "toEnd", ends)
		optional(w, v, "label", nil)
		color(w, v)
	}
	return ps
}
