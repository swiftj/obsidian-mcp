package bases

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Problem is one way a base breaks the Bases syntax.
type Problem struct {
	Path string `json:"path"` // such as views[1].groupBy.direction
	Msg  string `json:"msg"`
	// Warning marks something likely wrong that Obsidian still accepts.
	// Warnings never block an edit.
	Warning bool `json:"warning,omitempty"`
}

func (p Problem) String() string {
	s := p.Msg
	if p.Path != "" {
		s = p.Path + ": " + s
	}
	if p.Warning {
		s = "warning: " + s
	}
	return s
}

// defaultSummaries are the built-in summary names (bases-syntax.md
// §Default Summary Formulas).
var defaultSummaries = []string{"Average", "Min", "Max", "Sum", "Range", "Median", "Stddev",
	"Earliest", "Latest", "Checked", "Unchecked", "Empty", "Filled", "Unique"}

type checker struct {
	ps       []Problem
	formulas map[string]bool
	sums     map[string]bool
}

func (c *checker) add(path, format string, a ...any) {
	c.ps = append(c.ps, Problem{path, fmt.Sprintf(format, a...), false})
}

func (c *checker) warn(path, format string, a ...any) {
	c.ps = append(c.ps, Problem{path, fmt.Sprintf(format, a...), true})
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func kindName(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		return "a mapping"
	case yaml.SequenceNode:
		return "a list"
	case yaml.AliasNode:
		return "an alias"
	}
	if n.Tag == "!!null" {
		return "empty"
	}
	return "a " + strings.TrimPrefix(n.Tag, "!!")
}

func isString(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!str" }

// Validate checks the base against the Bases syntax (B-SCH-01 to
// B-VIEW-01). Expressions aren't parsed (D-013). Unknown keys are allowed:
// plugins and views may add them.
func (b *Base) Validate() []Problem {
	c := &checker{formulas: map[string]bool{}, sums: map[string]bool{}}
	r := b.root
	if r == nil || r.Kind == yaml.ScalarNode && r.Tag == "!!null" {
		return nil // an empty base shows every file
	}
	if r.Kind != yaml.MappingNode {
		c.add("", "a base must be a mapping, not %s", kindName(r))
		return c.ps
	}
	if f := get(r, "formulas"); f != nil {
		c.stringMap("formulas", f, c.formulas)
	}
	if s := get(r, "summaries"); s != nil {
		c.stringMap("summaries", s, c.sums)
	}
	if f := get(r, "filters"); f != nil {
		c.filter("filters", f)
	}
	if p := get(r, "properties"); p != nil {
		c.properties(p)
	}
	if v := get(r, "views"); v != nil {
		c.views(v)
	}
	return c.ps
}

// stringMap checks a mapping of names to expression strings (B-FRM-01,
// B-SUM-01).
func (c *checker) stringMap(path string, n *yaml.Node, names map[string]bool) {
	if n.Kind != yaml.MappingNode {
		c.add(path, "must be a mapping of names to formulas, not %s", kindName(n))
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i].Value, n.Content[i+1]
		names[k] = true
		if !isString(v) {
			c.add(join(path, k), "must be a formula string, not %s", kindName(v))
		}
	}
}

// filter checks a filter: a statement string, or a mapping with exactly one
// of and, or, not holding a list of filters (B-FLT-01).
func (c *checker) filter(path string, n *yaml.Node) {
	switch n.Kind {
	case yaml.ScalarNode:
		if !isString(n) {
			c.add(path, "a filter must be a statement string or an and/or/not mapping, not %s", kindName(n))
		}
	case yaml.MappingNode:
		if len(n.Content) != 2 {
			c.add(path, "a filter mapping must have exactly one of and, or, not")
			return
		}
		k, v := n.Content[0].Value, n.Content[1]
		if !slices.Contains([]string{"and", "or", "not"}, k) {
			c.add(path, "a filter mapping must have exactly one of and, or, not, not %q", k)
			return
		}
		if v.Kind != yaml.SequenceNode {
			c.add(join(path, k), "must be a list of filters, not %s", kindName(v))
			return
		}
		for i, f := range v.Content {
			c.filter(fmt.Sprintf("%s[%d]", join(path, k), i), f)
		}
	default:
		c.add(path, "a filter must be a statement string or an and/or/not mapping, not %s", kindName(n))
	}
}

// properties checks the property configuration (B-PROP-01).
func (c *checker) properties(n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		c.add("properties", "must be a mapping of property IDs to settings, not %s", kindName(n))
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		id, v := n.Content[i].Value, n.Content[i+1]
		path := join("properties", id)
		if v.Kind != yaml.MappingNode {
			c.add(path, "must be a mapping of settings such as displayName, not %s", kindName(v))
		} else if d := get(v, "displayName"); d != nil && d.Kind != yaml.ScalarNode {
			c.add(join(path, "displayName"), "must be text, not %s", kindName(d))
		}
		c.propertyID(path, id)
	}
}

// propertyID checks a property ID: name, note.name, file.name or
// formula.name, where a formula must exist.
func (c *checker) propertyID(path, id string) {
	if strings.TrimSpace(id) == "" {
		c.add(path, "a property ID can't be empty")
		return
	}
	if f, ok := strings.CutPrefix(id, "formula."); ok && !c.formulas[f] {
		c.warn(path, "no formula is named %q", f)
	}
}

// views checks the views list (B-VIEW-01, B-SUM-01).
func (c *checker) views(n *yaml.Node) {
	if n.Kind != yaml.SequenceNode {
		c.add("views", "must be a list of views, not %s", kindName(n))
		return
	}
	seen := map[string]bool{}
	for i, v := range n.Content {
		path := fmt.Sprintf("views[%d]", i)
		if v.Kind != yaml.MappingNode {
			c.add(path, "a view must be a mapping, not %s", kindName(v))
			continue
		}
		for _, key := range []string{"type", "name"} {
			f := get(v, key)
			switch {
			case f == nil:
				c.add(path, "%q is required", key)
			case !isString(f) || f.Value == "":
				c.add(join(path, key), "must be non-empty text")
			}
		}
		if name := scalar(get(v, "name")); name != "" {
			if seen[name] {
				c.add(join(path, "name"), "another view is already named %q", name)
			}
			seen[name] = true
		}
		if l := get(v, "limit"); l != nil && (l.Kind != yaml.ScalarNode || l.Tag != "!!int") {
			c.add(join(path, "limit"), "must be an integer")
		}
		if o := get(v, "order"); o != nil {
			if o.Kind != yaml.SequenceNode {
				c.add(join(path, "order"), "must be a list of property IDs")
			} else {
				for j, p := range o.Content {
					if !isString(p) {
						c.add(fmt.Sprintf("%s.order[%d]", path, j), "must be a property ID")
					} else {
						c.propertyID(fmt.Sprintf("%s.order[%d]", path, j), p.Value)
					}
				}
			}
		}
		if g := get(v, "groupBy"); g != nil {
			c.groupBy(join(path, "groupBy"), g)
		}
		if g := get(v, "groupOrder"); g != nil {
			if g.Kind != yaml.SequenceNode {
				c.add(join(path, "groupOrder"), "must be a list of group values")
			} else {
				for j, it := range g.Content {
					if it.Kind != yaml.ScalarNode {
						c.add(fmt.Sprintf("%s.groupOrder[%d]", path, j), "must be a value or null, not %s", kindName(it))
					}
				}
			}
		}
		if f := get(v, "filters"); f != nil {
			c.filter(join(path, "filters"), f)
		}
		if s := get(v, "summaries"); s != nil {
			c.viewSummaries(join(path, "summaries"), s)
		}
	}
}

func (c *checker) groupBy(path string, g *yaml.Node) {
	if g.Kind != yaml.MappingNode {
		c.add(path, "must be a mapping with property and direction")
		return
	}
	if p := get(g, "property"); p == nil || !isString(p) || p.Value == "" {
		c.add(path, `"property" is required`)
	} else {
		c.propertyID(join(path, "property"), p.Value)
	}
	if d := get(g, "direction"); d != nil && (d.Kind != yaml.ScalarNode || d.Value != "ASC" && d.Value != "DESC") {
		c.add(join(path, "direction"), "must be ASC or DESC")
	}
}

// viewSummaries checks a view's property → summary mapping (B-SUM-01).
func (c *checker) viewSummaries(path string, s *yaml.Node) {
	if s.Kind != yaml.MappingNode {
		c.add(path, "must be a mapping of property IDs to summary names, not %s", kindName(s))
		return
	}
	for i := 0; i+1 < len(s.Content); i += 2 {
		id, v := s.Content[i].Value, s.Content[i+1]
		p := join(path, id)
		c.propertyID(p, id)
		switch {
		case !isString(v):
			c.add(p, "must be a summary name, not %s", kindName(v))
		case !slices.Contains(defaultSummaries, v.Value) && !c.sums[v.Value]:
			c.add(p, "%q is neither a default summary (%s) nor in summaries", v.Value, strings.Join(defaultSummaries, ", "))
		}
	}
}
