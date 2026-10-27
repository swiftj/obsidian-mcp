// Package yamledit changes values in a YAML document by patching bytes. Each
// change rewrites one entry, a key with its value or a list item, and leaves
// every other byte alone. Comments inside a rewritten entry are lost; the
// result is re-parsed and checked to hold exactly the requested change
// (B-EDIT-01, D-013).
package yamledit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Error reports a refused change.
type Error struct {
	Path string
	Msg  string
}

func (e *Error) Error() string {
	if e.Path == "" {
		return "yaml: " + e.Msg
	}
	return "yaml: " + e.Path + ": " + e.Msg
}

// Options modify a change.
type Options struct {
	// KeyOrder ranks the keys of new mappings: listed keys come first, in
	// this order, then the rest sorted.
	KeyOrder []string
}

// Set sets the value at path, creating missing mapping keys. A path is a
// list of mapping keys (strings) and list indexes (ints); an index equal to
// the list's length appends. value may be nil, a bool, a number, a string,
// []any or map[string]any.
func Set(src []byte, path []any, value any, o Options) ([]byte, error) {
	return edit(src, path, opSet, 0, value, o)
}

// Insert inserts value into the list at path before index; an index of -1
// or the list's length appends.
func Insert(src []byte, path []any, index int, value any, o Options) ([]byte, error) {
	return edit(src, path, opInsert, index, value, o)
}

// Remove removes the key or list item at path.
func Remove(src []byte, path []any) ([]byte, error) {
	return edit(src, path, opRemove, 0, nil, Options{})
}

// PathString writes a path as `views[0].groupBy`.
func PathString(path []any) string {
	var b strings.Builder
	for _, s := range path {
		switch s := s.(type) {
		case int:
			fmt.Fprintf(&b, "[%d]", s)
		default:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			fmt.Fprint(&b, s)
		}
	}
	return b.String()
}

type op int

const (
	opSet op = iota
	opInsert
	opRemove
)

// frame is one step down the path: entry idx of container node. For a
// mapping idx is the key's index in Content, for a list the item's.
type frame struct {
	node *yaml.Node
	idx  int
}

type editor struct {
	src  []byte
	doc  *yaml.Node
	ls   []int // line starts
	unit int
	eol  string
	o    Options
}

func edit(src []byte, path []any, kind op, index int, value any, o Options) ([]byte, error) {
	e := &editor{src: src, o: o, eol: "\n"}
	if bytes.Contains(src, []byte("\r\n")) {
		e.eol = "\r\n"
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, &Error{Msg: "the file is not valid YAML: " + err.Error()}
	}
	empty := doc.Kind == 0
	if empty {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	e.doc, e.ls, e.unit = &doc, lineStarts(src), indentUnit(src)
	var nv *yaml.Node
	if kind != opRemove {
		var err error
		if nv, err = e.node(value); err != nil {
			return nil, &Error{Path: PathString(path), Msg: err.Error()}
		}
	}

	// Walk the path as far as it exists.
	cur := doc.Content[0]
	var frames []frame
	k := 0
	for ; k < len(path); k++ {
		j, ok, err := child(cur, path[k])
		if err != nil {
			return nil, &Error{Path: PathString(path[:k+1]), Msg: err.Error()}
		}
		if !ok {
			break
		}
		frames = append(frames, frame{cur, j})
		if cur.Kind == yaml.MappingNode {
			cur = cur.Content[j+1]
		} else {
			cur = cur.Content[j]
		}
	}
	at := PathString(path)
	var act action
	switch kind {
	case opSet:
		if k == len(path) {
			if k == 0 {
				return nil, &Error{Msg: "a path is required"}
			}
			f := frames[k-1]
			if f.node.Kind == yaml.MappingNode {
				f.node.Content[f.idx+1] = nv
			} else {
				f.node.Content[f.idx] = nv
			}
			act = action{kind: actReplace, frame: k - 1}
			break
		}
		// Build what's missing below cur.
		for i := len(path) - 1; i > k; i-- {
			s, ok := path[i].(string)
			if !ok {
				return nil, &Error{Path: PathString(path[:i+1]), Msg: "the list doesn't exist"}
			}
			nv = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode(s), nv}}
		}
		switch seg := path[k].(type) {
		case string:
			if cur.Kind != yaml.MappingNode {
				if k == 0 || !isEmptyScalar(cur) {
					return nil, &Error{Path: PathString(path[:k]), Msg: "the value is not a mapping"}
				}
				m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode(seg), nv}}
				e.setValue(frames[k-1], m)
				act = action{kind: actReplace, frame: k - 1}
				break
			}
			cur.Content = append(cur.Content, strNode(seg), nv)
			act = action{kind: actAppend, container: cur, frame: k - 1}
		case int:
			if cur.Kind != yaml.SequenceNode || seg != len(cur.Content) {
				return nil, &Error{Path: PathString(path[:k+1]), Msg: "the index is out of range"}
			}
			cur.Content = append(cur.Content, nv)
			act = action{kind: actAppend, container: cur, frame: k - 1}
		}
	case opInsert:
		if k < len(path) {
			return nil, &Error{Path: at, Msg: "there is no list here"}
		}
		if cur.Kind != yaml.SequenceNode {
			return nil, &Error{Path: at, Msg: "the value is not a list"}
		}
		if index < 0 || index >= len(cur.Content) {
			if index > len(cur.Content) {
				return nil, &Error{Path: at, Msg: fmt.Sprintf("index %d is out of range", index)}
			}
			cur.Content = append(cur.Content, nv)
			act = action{kind: actAppend, container: cur, frame: k - 1}
			break
		}
		cur.Content = slices.Insert(cur.Content, index, nv)
		act = action{kind: actInsert, container: cur, idx: index, frame: k - 1}
	case opRemove:
		if k < len(path) || k == 0 {
			return nil, &Error{Path: at, Msg: "there is nothing here to remove"}
		}
		f := frames[k-1]
		act = action{kind: actRemove, container: f.node, idx: f.idx, frame: k - 2}
		act.removedRange = [2]int{-1, -1}
		if r, ok := e.cleanRange(f.node, f.idx); ok {
			act.removedRange = r
		}
		if f.node.Kind == yaml.MappingNode {
			f.node.Content = slices.Delete(f.node.Content, f.idx, f.idx+2)
		} else {
			f.node.Content = slices.Delete(f.node.Content, f.idx, f.idx+1)
		}
	}
	out, err := e.render(act, frames)
	if err != nil {
		return nil, err
	}
	if err := e.verify(out); err != nil {
		return nil, &Error{Path: at, Msg: err.Error()}
	}
	return out, nil
}

func isEmptyScalar(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

func (e *editor) setValue(f frame, v *yaml.Node) {
	if f.node.Kind == yaml.MappingNode {
		f.node.Content[f.idx+1] = v
	} else {
		f.node.Content[f.idx] = v
	}
}

// child finds seg in n: the key's index for a mapping, the item's for a
// list. ok is false when it's missing.
func child(n *yaml.Node, seg any) (int, bool, error) {
	switch s := seg.(type) {
	case string:
		if n.Kind != yaml.MappingNode {
			if n.Kind == yaml.ScalarNode {
				return 0, false, nil
			}
			return 0, false, fmt.Errorf("a key needs a mapping")
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == s {
				return i, true, nil
			}
		}
		return 0, false, nil
	case int:
		if n.Kind != yaml.SequenceNode {
			return 0, false, fmt.Errorf("an index needs a list")
		}
		return s, s >= 0 && s < len(n.Content), nil
	}
	return 0, false, fmt.Errorf("a path segment must be a key or an index, not %T", seg)
}

type actKind int

const (
	actReplace actKind = iota // the entry at frame was changed
	actAppend                 // an entry was appended to container
	actInsert                 // an item was inserted into container at idx
	actRemove                 // the entry at idx was removed from container
)

// action is what changed in the tree. frame is the index of the frame
// whose entry holds the change; -1 means the top level.
type action struct {
	kind         actKind
	container    *yaml.Node
	idx          int
	frame        int
	removedRange [2]int
}

// render turns the in-memory change into a byte patch: the smallest entry
// that can be rewritten cleanly, or the whole document.
func (e *editor) render(a action, frames []frame) ([]byte, error) {
	switch a.kind {
	case actAppend:
		c := a.container
		n := entries(c)
		if c.Style&yaml.FlowStyle == 0 && n > 1 {
			if r, ok := e.cleanRange(c, entryIndex(c, n-2)); ok {
				col := e.col(c, entryIndex(c, n-2))
				text, err := e.entryText(c, entryIndex(c, n-1), col)
				if err != nil {
					return nil, err
				}
				prefix := ""
				if r[1] == len(e.src) && len(e.src) > 0 && e.src[len(e.src)-1] != '\n' {
					prefix, text = e.eol, strings.TrimSuffix(text, e.eol)
				}
				return splice(e.src, r[1], r[1], prefix+text), nil
			}
		}
		return e.replaceUp(a.frame, frames)
	case actInsert:
		c := a.container
		if c.Style&yaml.FlowStyle == 0 {
			if r, ok := e.cleanRange(c, a.idx+1); ok {
				text, err := e.entryText(c, a.idx, e.col(c, a.idx+1))
				if err != nil {
					return nil, err
				}
				return splice(e.src, r[0], r[0], text), nil
			}
		}
		return e.replaceUp(a.frame, frames)
	case actRemove:
		c := a.container
		if c.Style&yaml.FlowStyle == 0 && entries(c) > 0 && a.removedRange[0] >= 0 {
			return splice(e.src, a.removedRange[0], a.removedRange[1], ""), nil
		}
		return e.replaceUp(a.frame, frames)
	default:
		return e.replaceUp(a.frame, frames)
	}
}

// replaceUp rewrites the entry at frames[i], or the nearest one above it
// that can be rewritten cleanly, or the whole document.
func (e *editor) replaceUp(i int, frames []frame) ([]byte, error) {
	for ; i >= 0; i-- {
		f := frames[i]
		if f.node.Style&yaml.FlowStyle != 0 {
			continue
		}
		r, ok := e.cleanRange(f.node, f.idx)
		if !ok {
			continue
		}
		text, err := e.entryText(f.node, f.idx, e.col(f.node, f.idx))
		if err != nil {
			return nil, err
		}
		if r[1] == len(e.src) && !bytes.HasSuffix(e.src, []byte("\n")) {
			text = strings.TrimSuffix(text, e.eol)
		}
		return splice(e.src, r[0], r[1], text), nil
	}
	text, err := e.encode(e.doc.Content[0])
	if err != nil {
		return nil, err
	}
	return []byte(e.indent(text, 0)), nil
}

func entries(c *yaml.Node) int {
	if c.Kind == yaml.MappingNode {
		return len(c.Content) / 2
	}
	return len(c.Content)
}

func entryIndex(c *yaml.Node, i int) int {
	if c.Kind == yaml.MappingNode {
		return 2 * i
	}
	return i
}

// entryStart is the offset of the entry's key, or of its list dash.
func (e *editor) entryStart(c *yaml.Node, idx int) int {
	n := c.Content[idx]
	if n.Line == 0 {
		return -1 // a new node
	}
	off := e.offset(n.Line, n.Column)
	if c.Kind == yaml.SequenceNode {
		j := off - 1
		for j >= 0 && (e.src[j] == ' ' || e.src[j] == '\n' || e.src[j] == '\r') {
			j--
		}
		if j < 0 || e.src[j] != '-' {
			return -1
		}
		off = j
	}
	return off
}

func (e *editor) offset(line, col int) int {
	if line-1 >= len(e.ls) {
		return len(e.src)
	}
	return min(e.ls[line-1]+col-1, len(e.src))
}

func (e *editor) lineOf(off int) int {
	i, _ := slices.BinarySearch(e.ls, off+1)
	return i - 1
}

// col is the entry's column.
func (e *editor) col(c *yaml.Node, idx int) int {
	off := e.entryStart(c, idx)
	return off - e.ls[e.lineOf(off)]
}

// cleanRange returns the byte range of an existing entry whose key or dash
// starts its line: from the line start to the start of the line after its
// last line, not counting trailing blank or comment lines.
func (e *editor) cleanRange(c *yaml.Node, idx int) ([2]int, bool) {
	if idx < 0 || idx >= len(c.Content) {
		return [2]int{}, false
	}
	off := e.entryStart(c, idx)
	if off < 0 {
		return [2]int{}, false
	}
	l := e.lineOf(off)
	start := e.ls[l]
	if strings.Trim(string(e.src[start:off]), " ") != "" {
		return [2]int{}, false
	}
	col := off - start
	seqValue := c.Kind == yaml.MappingNode && idx+1 < len(c.Content) &&
		c.Content[idx+1].Kind == yaml.SequenceNode && c.Content[idx+1].Style&yaml.FlowStyle == 0
	end, pending := len(e.src), -1
	for j := l + 1; j < len(e.ls); j++ {
		text := e.line(j)
		trimmed := strings.TrimLeft(text, " ")
		if strings.TrimSpace(trimmed) == "" || strings.HasPrefix(trimmed, "#") {
			if pending < 0 {
				pending = e.ls[j]
			}
			continue
		}
		ind := len(text) - len(trimmed)
		if ind > col || ind == col && seqValue && (trimmed == "-" || strings.HasPrefix(trimmed, "- ")) {
			pending = -1
			continue
		}
		end = e.ls[j]
		break
	}
	if pending >= 0 {
		end = pending
	}
	return [2]int{start, end}, true
}

func (e *editor) line(j int) string {
	end := len(e.src)
	if j+1 < len(e.ls) {
		end = e.ls[j+1]
	}
	return strings.TrimRight(string(e.src[e.ls[j]:end]), "\r\n")
}

// entryText renders entry idx of c, indented by col, ending in a newline.
func (e *editor) entryText(c *yaml.Node, idx, col int) (string, error) {
	var n *yaml.Node
	if c.Kind == yaml.MappingNode {
		n = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{c.Content[idx], c.Content[idx+1]}}
	} else {
		n = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{c.Content[idx]}}
	}
	text, err := e.encode(n)
	if err != nil {
		return "", err
	}
	return e.indent(text, col), nil
}

// encode renders a node in block style without comments.
func (e *editor) encode(n *yaml.Node) (string, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(e.unit)
	if err := enc.Encode(uncommented(n)); err != nil {
		return "", &Error{Msg: "the value can't be written as YAML: " + err.Error()}
	}
	if err := enc.Close(); err != nil {
		return "", &Error{Msg: err.Error()}
	}
	return b.String(), nil
}

func (e *editor) indent(text string, col int) string {
	lines := strings.SplitAfter(text, "\n")
	pad := strings.Repeat(" ", col)
	var b strings.Builder
	for _, l := range lines {
		if l == "" {
			continue
		}
		body := strings.TrimSuffix(l, "\n")
		if body != "" {
			b.WriteString(pad)
		}
		b.WriteString(body)
		if strings.HasSuffix(l, "\n") {
			b.WriteString(e.eol)
		}
	}
	return b.String()
}

func uncommented(n *yaml.Node) *yaml.Node {
	c := *n
	c.HeadComment, c.LineComment, c.FootComment = "", "", ""
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, k := range n.Content {
		c.Content[i] = uncommented(k)
	}
	return &c
}

// verify checks that out parses to the edited tree.
func (e *editor) verify(out []byte) error {
	var want, got any
	if err := e.doc.Decode(&want); err != nil {
		return fmt.Errorf("the edit can't be checked: %v", err)
	}
	if err := yaml.Unmarshal(out, &got); err != nil {
		return fmt.Errorf("the edit would break the YAML; nothing was changed")
	}
	if want == nil {
		want = map[string]any{}
	}
	if got == nil {
		got = map[string]any{}
	}
	if !reflect.DeepEqual(want, got) {
		return fmt.Errorf("the edit would change other content; nothing was changed")
	}
	return nil
}

func splice(src []byte, start, end int, text string) []byte {
	return slices.Concat(src[:start:start], []byte(text), src[end:])
}

func lineStarts(src []byte) []int {
	ls := []int{0}
	for i, c := range src {
		if c == '\n' && i+1 < len(src) {
			ls = append(ls, i+1)
		}
	}
	return ls
}

// indentUnit is the first indentation the document uses, or 2.
func indentUnit(src []byte) int {
	for l := range strings.SplitSeq(string(src), "\n") {
		t := strings.TrimLeft(l, " ")
		if t == "" || t[0] == '#' || strings.TrimSpace(t) == "" || len(t) == len(l) {
			continue
		}
		return max(2, len(l)-len(t))
	}
	return 2
}

func strNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// node converts a Go value to a YAML node.
func (e *editor) node(v any) (*yaml.Node, error) {
	switch x := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(x)}, nil
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(x)}, nil
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(x, 10)}, nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, fmt.Errorf("numbers must be finite")
		}
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(x), 10)}, nil
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(x, 'g', -1, 64)}, nil
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: x.String()}, nil
		}
		f, err := x.Float64()
		if err != nil {
			return nil, err
		}
		return e.node(f)
	case string:
		if !utf8.ValidString(x) {
			return nil, fmt.Errorf("the text is not valid UTF-8")
		}
		return strNode(x), nil
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, it := range x {
			c, err := e.node(it)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, c)
		}
		return n, nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			if !utf8.ValidString(k) {
				return nil, fmt.Errorf("a key is not valid UTF-8")
			}
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b string) int {
			ra, rb := rank(e.o.KeyOrder, a), rank(e.o.KeyOrder, b)
			if ra != rb {
				return ra - rb
			}
			return strings.Compare(a, b)
		})
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range keys {
			c, err := e.node(x[k])
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, strNode(k), c)
		}
		return n, nil
	}
	return nil, fmt.Errorf("a value can't be a %T", v)
}

func rank(order []string, k string) int {
	if i := slices.Index(order, k); i >= 0 {
		return i
	}
	return len(order)
}
