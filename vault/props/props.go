// Package props reads, type-checks and patches note properties (YAML or
// JSON frontmatter). A patch rewrites only the lines of the key it changes;
// every other byte of the note stays identical.
package props

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/swiftj/obsidian-mcp/vault/ofm"
	"go.yaml.in/yaml/v3"
)

// Type is a property type, as stored in .obsidian/types.json.
type Type string

const (
	Text      Type = "text"
	Multitext Type = "multitext" // shown as "List"
	Number    Type = "number"
	Checkbox  Type = "checkbox"
	Date      Type = "date"
	Datetime  Type = "datetime"
	Tags      Type = "tags"
	Aliases   Type = "aliases"
	Unknown   Type = "unknown" // nested values and other shapes Obsidian can't edit
)

// Types maps property names to their recorded types (types.json).
type Types map[string]Type

// ParseTypes decodes a types.json file: {"types": {name: type}} (P-TYPE-01).
func ParseTypes(data []byte) (Types, error) {
	var f struct {
		Types map[string]Type `json:"types"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("props: types.json: %w", err)
	}
	if f.Types == nil {
		f.Types = Types{}
	}
	return f.Types, nil
}

// deprecated maps the keys deprecated in Obsidian 1.4 to their plural forms
// (P-KEY-02).
var deprecated = map[string]string{"tag": "tags", "alias": "aliases", "cssclass": "cssclasses"}

// Canonical returns the name a key is read as: the plural form of a
// deprecated key, otherwise the key itself.
func Canonical(key string) string {
	if p, ok := deprecated[key]; ok {
		return p
	}
	return key
}

// Property is one top-level frontmatter key.
type Property struct {
	Key      string // as written
	Name     string // Canonical(Key)
	Value    any    // string, int64, float64, bool, nil, []any or map[string]any; dates stay strings
	Raw      string // the value's source text, for YAML
	Inferred Type
	// Lines is the key's whole entry: from the start of its line to the end
	// of its value's last line, including the line ending. Zero for JSON.
	Lines ofm.Span

	node *yaml.Node
	key  *yaml.Node
}

// Link is a wikilink inside a frontmatter string value (O-LINK-06).
type Link struct {
	Path string // the key, plus ".N" for a list item: "rel", "rels.0"
	ofm.Link
}

// Frontmatter is a note's parsed properties.
type Frontmatter struct {
	Present    bool // the note has a frontmatter block
	JSON       bool
	Props      []Property
	Duplicates []string // keys that appear more than once (P-KEY-01)
	Err        error    // the YAML or JSON is invalid; Props is then empty
	Links      []Link

	doc       *ofm.Doc
	lineStart []int // absolute offsets of the body's line starts
	flow      bool  // the YAML root mapping is in flow style
	// unpatchable says why the source can't be patched line by line, or "".
	unpatchable string
}

// Get returns the property read as name, or nil.
func (f *Frontmatter) Get(name string) *Property {
	for i := range f.Props {
		if f.Props[i].Name == name {
			return &f.Props[i]
		}
	}
	return nil
}

// Read parses the frontmatter of a scanned note. It never fails; problems
// are reported in Err and Duplicates.
func Read(d *ofm.Doc) *Frontmatter {
	f := &Frontmatter{doc: d}
	if d.Frontmatter == nil {
		return f
	}
	f.Present, f.JSON = true, d.Frontmatter.JSON
	body := d.Src[d.Frontmatter.Body.Start:d.Frontmatter.Body.End]
	if f.JSON {
		f.readJSON(body)
		return f
	}
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		f.Err = fmt.Errorf("props: invalid YAML frontmatter: %w", err)
		return f
	}
	if len(root.Content) == 0 {
		return f // empty
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		f.Err = errors.New("props: frontmatter is not a mapping of properties")
		return f
	}
	f.flow = m.Style&yaml.FlowStyle != 0
	f.unpatchable = unpatchable(body)
	if len(m.Content) > 0 && m.Content[0].Column != 1 && f.unpatchable == "" {
		f.unpatchable = "the properties are indented"
	}
	f.lineStart = lineStarts(d.Src, d.Frontmatter.Body)
	seen := map[string]int{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		p := Property{Key: k.Value, Name: Canonical(k.Value), node: v, key: k}
		p.Value = decode(v)
		p.Inferred = infer(p.Name, v)
		if !f.flow && f.unpatchable == "" {
			p.Lines = f.entrySpan(m, i)
			p.Raw = f.rawValue(p)
		}
		seen[p.Name]++
		if seen[p.Name] == 2 {
			f.Duplicates = append(f.Duplicates, p.Name)
		}
		f.Props = append(f.Props, p)
		f.collectLinks(p.Key, v)
	}
	return f
}

func (f *Frontmatter) readJSON(body []byte) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		f.Err = errors.New("props: invalid JSON frontmatter")
		return
	}
	seen := map[string]int{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			f.Err = fmt.Errorf("props: invalid JSON frontmatter: %w", err)
			f.Props = nil
			return
		}
		key, _ := kt.(string)
		var raw any
		if err := dec.Decode(&raw); err != nil {
			f.Err = fmt.Errorf("props: invalid JSON frontmatter: %w", err)
			f.Props = nil
			return
		}
		v := fromJSON(raw)
		p := Property{Key: key, Name: Canonical(key), Value: v, Inferred: inferValue(Canonical(key), v)}
		seen[p.Name]++
		if seen[p.Name] == 2 {
			f.Duplicates = append(f.Duplicates, p.Name)
		}
		f.Props = append(f.Props, p)
	}
}

func fromJSON(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		fl, _ := x.Float64()
		return fl
	case []any:
		for i := range x {
			x[i] = fromJSON(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = fromJSON(x[k])
		}
	}
	return v
}

// decode converts a YAML node to a plain value. Timestamps keep their written
// text (P-DATE-01).
func decode(n *yaml.Node) any {
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		return decode(n.Alias)
	}
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!null":
			return nil
		case "!!bool":
			var b bool
			if n.Decode(&b) == nil {
				return b
			}
		case "!!int":
			var i int64
			if n.Decode(&i) == nil {
				return i
			}
		case "!!float":
			var fl float64
			if n.Decode(&fl) == nil {
				return fl
			}
		}
		return n.Value
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, decode(c))
		}
		return out
	case yaml.MappingNode:
		out := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			out[n.Content[i].Value] = decode(n.Content[i+1])
		}
		return out
	}
	return nil
}

var (
	dateRE     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	datetimeRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$`)
)

// infer returns the type Obsidian infers for a value with no recorded type
// (P-TYPE-06, the D-006 table).
func infer(name string, n *yaml.Node) Type {
	if t, ok := builtinTypes[name]; ok {
		return t
	}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	switch n.Kind {
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				return Unknown
			}
		}
		return Multitext
	case yaml.MappingNode:
		return Unknown
	}
	switch n.Tag {
	case "!!int", "!!float":
		return Number
	case "!!bool":
		return Checkbox
	case "!!null":
		return Text
	}
	return inferString(n.Value)
}

// inferValue is infer for decoded JSON values.
func inferValue(name string, v any) Type {
	if t, ok := builtinTypes[name]; ok {
		return t
	}
	switch x := v.(type) {
	case int64, float64:
		return Number
	case bool:
		return Checkbox
	case nil:
		return Text
	case string:
		return inferString(x)
	case []any:
		for _, e := range x {
			switch e.(type) {
			case []any, map[string]any:
				return Unknown
			}
		}
		return Multitext
	}
	return Unknown
}

func inferString(s string) Type {
	switch {
	case dateRE.MatchString(s):
		return Date
	case datetimeRE.MatchString(s):
		return Datetime // with seconds, Obsidian 1.14.4 infers text (D-006)
	}
	return Text
}

var builtinTypes = map[string]Type{"tags": Tags, "aliases": Aliases, "cssclasses": Multitext}

// TypeOf returns a property's effective type: recorded, else built-in, else
// inferred.
func TypeOf(p *Property, types Types) Type {
	if t, ok := types[p.Name]; ok && t != "" {
		return t
	}
	return p.Inferred
}

// Tags returns the tags the frontmatter "tags" key gives (O-TAG-02): one per
// list item with a leading "#" removed, one for a single-word scalar, and
// none for a scalar with a space or comma. The singular "tag" key gives none.
func (f *Frontmatter) Tags() []string {
	var out []string
	for _, p := range f.Props {
		if p.Key != "tags" {
			continue
		}
		switch v := p.Value.(type) {
		case string:
			if v != "" && !strings.ContainsAny(v, " ,\t") {
				out = append(out, strings.TrimPrefix(v, "#"))
			}
		case []any:
			for _, e := range v {
				if s := scalarString(e); s != "" {
					out = append(out, strings.TrimPrefix(s, "#"))
				}
			}
		}
	}
	return out
}

func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// collectLinks reports wikilinks in string values and list items (O-LINK-06).
func (f *Frontmatter) collectLinks(key string, n *yaml.Node) {
	add := func(path string, s *yaml.Node) {
		if s.Kind != yaml.ScalarNode || s.Tag != "!!str" || !strings.Contains(s.Value, "[[") {
			return
		}
		for _, l := range ofm.Parse([]byte(s.Value)).Links {
			if !l.Markdown {
				f.Links = append(f.Links, Link{Path: path, Link: l})
			}
		}
	}
	switch n.Kind {
	case yaml.ScalarNode:
		add(key, n)
	case yaml.SequenceNode:
		for i, c := range n.Content {
			add(key+"."+strconv.Itoa(i), c)
		}
	}
}

// unpatchable reports YAML whose lines can't be mapped one to one onto the
// source: a bare carriage return (a line break to YAML), or a document
// marker that starts a second YAML document.
func unpatchable(body []byte) string {
	if bytes.Contains(bytes.ReplaceAll(body, []byte("\r\n"), nil), []byte("\r")) {
		return "the frontmatter contains a carriage return without a line feed"
	}
	for ln := range bytes.SplitSeq(body, []byte("\n")) {
		ln = bytes.TrimRight(ln, "\r")
		for _, m := range []string{"---", "..."} {
			if bytes.HasPrefix(ln, []byte(m)) && (len(ln) == 3 || ln[3] == ' ' || ln[3] == '\t') {
				return "the frontmatter contains a YAML document marker (" + m + ")"
			}
		}
	}
	return ""
}

// --- source positions ---

func lineStarts(src []byte, body ofm.Span) []int {
	out := []int{body.Start}
	for i := body.Start; i < body.End; i++ {
		if src[i] == '\n' && i+1 < body.End {
			out = append(out, i+1)
		}
	}
	return out
}

// lineAt returns the offset where body line n (1-based) starts.
func (f *Frontmatter) lineAt(n int) int {
	if n-1 < len(f.lineStart) {
		return f.lineStart[n-1]
	}
	return f.doc.Frontmatter.Body.End
}

// entrySpan is the lines of the i-th key of mapping m: from its line to the
// next key's line, minus trailing blank lines and column-0 comments, which
// belong to what follows.
func (f *Frontmatter) entrySpan(m *yaml.Node, i int) ofm.Span {
	start := f.lineAt(m.Content[i].Line)
	end := f.doc.Frontmatter.Body.End
	if i+2 < len(m.Content) {
		end = f.lineAt(m.Content[i+2].Line)
	}
	src := f.doc.Src
	firstEnd := lineEnd(src, start, end)
	for end > firstEnd {
		ls := prevLineStart(src, start, end)
		t := bytes.TrimRight(src[ls:end], "\r\n")
		if len(bytes.TrimSpace(t)) != 0 && t[0] != '#' {
			break
		}
		end = ls
	}
	return ofm.Span{Start: start, End: end}
}

// rawValue returns the value's source text: from the value's position to
// the end of the entry, trimmed.
func (f *Frontmatter) rawValue(p Property) string {
	v := p.node
	at := f.offsetOf(v.Line, v.Column)
	if at < p.Lines.Start || at > p.Lines.End {
		return ""
	}
	return strings.TrimSpace(string(f.doc.Src[at:p.Lines.End]))
}

// offsetOf converts a YAML line and (rune) column, both 1-based, to an
// absolute byte offset.
func (f *Frontmatter) offsetOf(line, col int) int {
	src := f.doc.Src
	start := f.lineAt(line)
	end := lineEnd(src, start, f.doc.Frontmatter.Body.End)
	i := start
	for c := 1; c < col && i < end; c++ {
		_, n := utf8.DecodeRune(src[i:end])
		i += n
	}
	return i
}

// lineEnd returns the offset just past the line ending of the line starting
// at i (bounded by limit).
func lineEnd(src []byte, i, limit int) int {
	if k := bytes.IndexByte(src[i:limit], '\n'); k >= 0 {
		return i + k + 1
	}
	return limit
}

// prevLineStart returns the start of the line that ends at end (exclusive,
// after its "\n"), not before floor.
func prevLineStart(src []byte, floor, end int) int {
	k := end - 1
	if k >= floor && src[k] == '\n' {
		k--
	}
	for k >= floor && src[k] != '\n' {
		k--
	}
	return k + 1
}
