// Package bases reads, validates and edits Obsidian Bases files (.base):
// YAML holding filters, formulas, properties, summaries and views. Queries
// are never evaluated (B-NOQ-01). Edits patch the YAML in place
// (B-EDIT-01).
package bases

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/swiftj/obsidian-mcp/internal/yamledit"
	"github.com/swiftj/obsidian-mcp/vault/ofm"
	"go.yaml.in/yaml/v3"
)

// Base is a parsed .base file or base code block.
type Base struct {
	root *yaml.Node // the top-level node; nil for an empty file
}

// Parse parses a base. It fails only when the bytes aren't YAML; the rest
// is for Validate.
func Parse(data []byte) (*Base, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, &Error{Msg: "the file is not valid YAML: " + strings.TrimPrefix(err.Error(), "yaml: ")}
	}
	b := &Base{}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		b.root = doc.Content[0]
	}
	return b, nil
}

// View is a view's main settings, for summaries.
type View struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Limit     int      `json:"limit,omitempty"`
	GroupBy   string   `json:"group_by,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Order     []string `json:"order,omitempty"`
	Filtered  bool     `json:"filtered,omitempty"`
}

// Named is a name with its expression: a formula or a summary.
type Named struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func get(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func scalar(n *yaml.Node) string {
	if n != nil && n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return ""
}

func named(n *yaml.Node) []Named {
	var out []Named
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, Named{n.Content[i].Value, scalar(n.Content[i+1])})
	}
	return out
}

// Views returns the views in file order.
func (b *Base) Views() []View {
	vs := get(b.root, "views")
	if vs == nil || vs.Kind != yaml.SequenceNode {
		return nil
	}
	var out []View
	for _, v := range vs.Content {
		w := View{Name: scalar(get(v, "name")), Type: scalar(get(v, "type")), Filtered: get(v, "filters") != nil}
		w.Limit, _ = strconv.Atoi(scalar(get(v, "limit")))
		if g := get(v, "groupBy"); g != nil {
			w.GroupBy, w.Direction = scalar(get(g, "property")), scalar(get(g, "direction"))
		}
		if o := get(v, "order"); o != nil && o.Kind == yaml.SequenceNode {
			for _, p := range o.Content {
				w.Order = append(w.Order, scalar(p))
			}
		}
		out = append(out, w)
	}
	return out
}

// Formulas returns the formulas in file order.
func (b *Base) Formulas() []Named { return named(get(b.root, "formulas")) }

// Summaries returns the custom summary formulas in file order.
func (b *Base) Summaries() []Named { return named(get(b.root, "summaries")) }

// Properties returns the configured property IDs in file order.
func (b *Base) Properties() []string {
	var out []string
	for _, n := range named(get(b.root, "properties")) {
		out = append(out, n.Name)
	}
	return out
}

// HasFilters reports whether the base has top-level filters.
func (b *Base) HasFilters() bool { return get(b.root, "filters") != nil }

// Block is a ```base code block in a note (B-EMB-01).
type Block struct {
	Line int    // 1-based line of the opening fence
	Text string // the YAML inside the fences
}

// CodeBlocks returns a note's base code blocks. Blocks inside quotes or
// lists, whose lines carry markers, are skipped.
func CodeBlocks(src []byte, d *ofm.Doc) []Block {
	var out []Block
	for _, c := range d.Code {
		if c.Kind != ofm.CodeFenced || firstField(c.Info) != "base" {
			continue
		}
		ls := strings.LastIndexByte(string(src[:c.Start]), '\n') + 1
		if strings.TrimSpace(string(src[ls:c.Start])) != "" {
			continue
		}
		lines := strings.Split(string(src[c.Start:c.End]), "\n")[1:]
		if n := len(lines); n > 0 {
			if t := strings.TrimSpace(lines[n-1]); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
				lines = lines[:n-1] // the closing fence
			}
		}
		text := strings.Join(lines, "\n")
		if len(lines) > 0 {
			text += "\n"
		}
		out = append(out, Block{Line: strings.Count(string(src[:c.Start]), "\n") + 1, Text: text})
	}
	return out
}

func firstField(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

// Error reports an unreadable base or a refused edit.
type Error struct {
	Op       int // the failing op, from 1; 0 or -1 for none
	Msg      string
	Problems []Problem
}

func (e *Error) Error() string {
	msg := "base: "
	if e.Op > 0 {
		msg += fmt.Sprintf("op %d: ", e.Op)
	}
	msg += e.Msg
	for _, p := range e.Problems {
		msg += "; " + p.String()
	}
	return msg
}

// OpKind is an edit_base operation.
type OpKind string

const (
	Set    OpKind = "set"
	Remove OpKind = "remove"
	Insert OpKind = "insert"
)

// Op is one base edit. Path segments are keys (strings) and list indexes
// (integers, which may arrive as float64 from JSON). Under "views", a
// string segment names a view.
type Op struct {
	Kind  OpKind
	Path  []any
	Index int // Insert: before this index; -1 appends
	Value any
}

// Result is the outcome of Apply.
type Result struct {
	Data     []byte
	Existing []Problem // problems already in the file, and warnings (D-013)
}

// keyOrder orders the keys of new mappings: the order the docs use.
var keyOrder = []string{"filters", "formulas", "properties", "summaries", "views",
	"type", "name", "limit", "groupBy", "property", "direction", "groupOrder",
	"and", "or", "not", "order", "displayName"}

// Apply applies ops in order. The result must not add problems to the file.
func Apply(data []byte, ops []Op) (*Result, error) {
	b, err := Parse(data)
	if err != nil {
		return nil, err
	}
	before := b.Validate()
	src := data
	for i, op := range ops {
		path, err := b.resolve(op.Path)
		if err == nil {
			o := yamledit.Options{KeyOrder: keyOrder}
			switch op.Kind {
			case Set:
				src, err = yamledit.Set(src, path, op.Value, o)
			case Remove:
				src, err = yamledit.Remove(src, path)
			case Insert:
				src, err = yamledit.Insert(src, path, op.Index, op.Value, o)
			default:
				err = fmt.Errorf("unknown op %q; use set, remove or insert", op.Kind)
			}
		}
		if err != nil {
			return nil, &Error{Op: i + 1, Msg: strings.TrimPrefix(err.Error(), "yaml: ")}
		}
		if b, err = Parse(src); err != nil {
			return nil, err
		}
	}
	existing, added := diffProblems(before, b.Validate())
	var blocking []Problem
	for _, p := range added {
		if p.Warning {
			existing = append(existing, p)
		} else {
			blocking = append(blocking, p)
		}
	}
	if added = blocking; len(added) > 0 {
		return nil, &Error{Op: -1, Msg: "the result isn't a valid base", Problems: added}
	}
	return &Result{Data: src, Existing: existing}, nil
}

// resolve turns JSON numbers into indexes and view names into view
// indexes.
func (b *Base) resolve(path []any) ([]any, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("a path is required")
	}
	out := make([]any, len(path))
	for i, s := range path {
		switch x := s.(type) {
		case string:
			out[i] = x
		case int:
			out[i] = x
		case float64:
			if x != float64(int(x)) {
				return nil, fmt.Errorf("path segment %v is not an index", x)
			}
			out[i] = int(x)
		default:
			return nil, fmt.Errorf("path segment %v must be a key or an index", s)
		}
	}
	if name, ok := out[1%len(out)].(string); len(out) > 1 && ok && out[0] == "views" {
		i := slices.IndexFunc(b.Views(), func(v View) bool { return v.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("no view is named %q", name)
		}
		out[1] = i
	}
	return out, nil
}

func diffProblems(before, after []Problem) (existing, added []Problem) {
	left := map[Problem]int{}
	for _, p := range before {
		left[p]++
	}
	for _, p := range after {
		if left[p] > 0 {
			left[p]--
			existing = append(existing, p)
		} else {
			added = append(added, p)
		}
	}
	return existing, added
}
