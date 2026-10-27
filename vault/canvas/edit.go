package canvas

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// OpKind is an edit_canvas operation.
type OpKind string

const (
	AddNode    OpKind = "add_node"
	UpdateNode OpKind = "update_node"
	RemoveNode OpKind = "remove_node"
	AddEdge    OpKind = "add_edge"
	UpdateEdge OpKind = "update_edge"
	RemoveEdge OpKind = "remove_edge"
)

// Op is one canvas edit (C-EDIT-01).
type Op struct {
	Kind OpKind
	// ID names the node or edge. For an add it may be empty, and a unique
	// ID is generated.
	ID string
	// Set holds the fields to add or change: strings, and integers for x,
	// y, width and height.
	Set map[string]any
	// Unset lists fields to remove (updates only).
	Unset []string
}

// Result is the outcome of Apply.
type Result struct {
	Data []byte
	// Added holds the IDs of added nodes and edges, in op order.
	Added []string
	// RemovedEdges holds the edges removed along with their nodes.
	RemovedEdges []string
	// Existing holds problems that were already in the file. They don't
	// block an edit (D-013).
	Existing []Problem
}

// Error reports a refused edit. Op is the index of the failing op, or -1
// when the result as a whole is invalid; Problems then lists what the edits
// would introduce.
type Error struct {
	Op       int
	Msg      string
	Problems []Problem
}

func (e *Error) Error() string {
	msg := "canvas: "
	if e.Op >= 0 {
		msg += fmt.Sprintf("op %d: ", e.Op+1)
	}
	msg += e.Msg
	for _, p := range e.Problems {
		msg += "; " + p.String()
	}
	return msg
}

// Field orders for new elements: the order the spec lists them.
var (
	nodeKeys = []string{"id", "type", "x", "y", "width", "height", "color", "text", "file", "subpath", "url", "label", "background", "backgroundStyle"}
	edgeKeys = []string{"id", "fromNode", "fromSide", "fromEnd", "toNode", "toSide", "toEnd", "color", "label"}
	intKeys  = []string{"x", "y", "width", "height"}
)

// Template is a new, empty canvas: tab-indented (C-PRES-01).
const Template = "{\n\t\"nodes\":[],\n\t\"edges\":[]\n}"

// New returns a new canvas holding the added nodes and edges.
func New(ops []Op) (*Result, error) {
	for i, op := range ops {
		if op.Kind != AddNode && op.Kind != AddEdge {
			return nil, &Error{Op: i, Msg: "a new canvas can only add nodes and edges"}
		}
	}
	return Apply([]byte(Template), ops)
}

// Apply applies ops in order and returns the patched canvas. Bytes outside
// the edited nodes and edges don't change, new elements are appended (so
// z-order is kept, C-ORD-01) in the style of their neighbours, and the
// result must not add problems to the file (C-EDIT-01).
func Apply(data []byte, ops []Op) (*Result, error) {
	c, err := Parse(data)
	if err != nil {
		return nil, err
	}
	before := c.Validate()
	e := &editor{c: c, res: &Result{}}
	for i, op := range ops {
		err := e.apply(op)
		if err == nil {
			err = e.err
		}
		if err != nil {
			return nil, &Error{Op: i, Msg: err.Error()}
		}
	}
	after := e.c.Validate()
	existing, added := diffProblems(before, after)
	if len(added) > 0 {
		return nil, &Error{Op: -1, Msg: "the result isn't valid JSON Canvas 1.0", Problems: added}
	}
	e.res.Data, e.res.Existing = e.c.src, existing
	return e.res, nil
}

// diffProblems splits after into problems that were in before and new ones.
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

type editor struct {
	c   *Canvas
	res *Result
	err error // set by a failed splice
}

func (e *editor) apply(op Op) error {
	arr, keys, noun := "nodes", nodeKeys, "node"
	if op.Kind == AddEdge || op.Kind == UpdateEdge || op.Kind == RemoveEdge {
		arr, keys, noun = "edges", edgeKeys, "edge"
	}
	if _, ok := op.Set["id"]; ok {
		return fmt.Errorf(`set "id" through the op's id; IDs can't be changed`)
	}
	switch op.Kind {
	case AddNode, AddEdge:
		if len(op.Unset) > 0 {
			return fmt.Errorf("unset applies only to updates")
		}
		id := op.ID
		if id == "" {
			id = e.newID()
		} else if e.used(id) {
			return fmt.Errorf("id %q is already used", id)
		}
		members := [][2]string{{"id", quote(id)}}
		for _, k := range keys[1:] {
			v, ok := op.Set[k]
			if !ok {
				continue
			}
			raw, err := encode(k, v)
			if err != nil {
				return err
			}
			members = append(members, [2]string{k, raw})
		}
		if err := unknownKeys(op.Set, keys, noun); err != nil {
			return err
		}
		if err := e.appendItem(arr, members); err != nil {
			return err
		}
		e.res.Added = append(e.res.Added, id)
	case UpdateNode, UpdateEdge:
		if e.find(arr, op.ID) < 0 {
			return fmt.Errorf("no %s has id %q", noun, op.ID)
		}
		if err := unknownKeys(op.Set, keys, noun); err != nil {
			return err
		}
		for _, k := range keys[1:] {
			v, ok := op.Set[k]
			if !ok {
				continue
			}
			raw, err := encode(k, v)
			if err != nil {
				return err
			}
			obj := e.c.array(arr)[e.find(arr, op.ID)]
			if i := obj.index(k); i >= 0 {
				m := obj.members[i]
				e.splice(m.val.start, m.val.end, raw)
			} else {
				e.appendMember(obj, k, raw)
			}
		}
		for _, k := range op.Unset {
			if k == "id" {
				return fmt.Errorf(`"id" can't be unset`)
			}
			obj := e.c.array(arr)[e.find(arr, op.ID)]
			if i := obj.index(k); i >= 0 {
				e.removeMember(obj, i)
			}
		}
	case RemoveNode, RemoveEdge:
		i := e.find(arr, op.ID)
		if i < 0 {
			return fmt.Errorf("no %s has id %q", noun, op.ID)
		}
		e.removeItem(e.c.root.get(arr), i)
		if op.Kind == RemoveNode {
			for { // its edges go with it (C-EDIT-01)
				j := slices.IndexFunc(e.c.array("edges"), func(v *value) bool {
					return str(v, "fromNode") == op.ID || str(v, "toNode") == op.ID
				})
				if j < 0 {
					break
				}
				e.res.RemovedEdges = append(e.res.RemovedEdges, str(e.c.array("edges")[j], "id"))
				e.removeItem(e.c.root.get("edges"), j)
			}
		}
	default:
		return fmt.Errorf("unknown op %q", op.Kind)
	}
	return nil
}

func unknownKeys(set map[string]any, keys []string, noun string) error {
	for k := range set {
		if !slices.Contains(keys, k) {
			return fmt.Errorf("%q is not a %s field; use one of %v", k, noun, keys[1:])
		}
	}
	return nil
}

// encode renders a field value as JSON.
func encode(key string, v any) (string, error) {
	if slices.Contains(intKeys, key) {
		var n int64
		switch x := v.(type) {
		case int:
			n = int64(x)
		case int64:
			n = x
		case float64:
			if x != math.Trunc(x) || math.Abs(x) > 1<<53 {
				return "", fmt.Errorf("%q must be an integer", key)
			}
			n = int64(x)
		case json.Number:
			i, err := x.Int64()
			if err != nil {
				return "", fmt.Errorf("%q must be an integer", key)
			}
			n = i
		default:
			return "", fmt.Errorf("%q must be an integer", key)
		}
		return strconv.FormatInt(n, 10), nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%q must be a string", key)
	}
	if !utf8.ValidString(s) {
		return "", fmt.Errorf("%q is not valid UTF-8", key)
	}
	return quote(s), nil
}

func (e *editor) used(id string) bool {
	return e.find("nodes", id) >= 0 || e.find("edges", id) >= 0
}

func (e *editor) find(arr, id string) int {
	return slices.IndexFunc(e.c.array(arr), func(v *value) bool { return str(v, "id") == id })
}

// newID returns 16 random hex digits not used by any node or edge
// (C-NODE-02, D-013).
func (e *editor) newID() string {
	for {
		var b [8]byte
		_, _ = rand.Read(b[:])
		if id := hex.EncodeToString(b[:]); !e.used(id) {
			return id
		}
	}
}

// splice replaces src[start:end] and re-parses. Every edit replaces whole
// JSON values or the separators between them, so the result stays valid.
func (e *editor) splice(start, end int, text string) {
	src := slices.Concat(e.c.src[:start:start], []byte(text), e.c.src[end:])
	c, err := Parse(src)
	if err != nil {
		if e.err == nil {
			e.err = fmt.Errorf("an edit produced invalid JSON (a bug); nothing was changed")
		}
		return
	}
	e.c = c
}

// style is how an object is laid out: the text before the first member,
// between a key and its value, between members, and after the last.
type style struct{ open, kv, sep, close string }

func (s style) render(members [][2]string) string {
	parts := make([]string, len(members))
	for i, m := range members {
		parts[i] = quote(m[0]) + s.kv + m[1]
	}
	return "{" + s.open + strings.Join(parts, s.sep) + s.close + "}"
}

// styleOf reads an object's layout from its bytes.
func (e *editor) styleOf(obj *value) style {
	src, ms := e.c.src, obj.members
	st := style{kv: ":", sep: ","}
	if len(ms) == 0 {
		return st
	}
	st.kv = string(src[ms[0].keyEnd:ms[0].val.start])
	st.open = string(src[obj.start+1 : ms[0].keyStart])
	st.close = string(src[ms[len(ms)-1].val.end : obj.end-1])
	if len(ms) >= 2 {
		st.sep = string(src[ms[len(ms)-2].val.end:ms[len(ms)-1].keyStart])
	} else if strings.Contains(st.open, "\n") {
		st.sep = "," + st.open
	}
	return st
}

func (e *editor) appendMember(obj *value, key, raw string) {
	st := e.styleOf(obj)
	if len(obj.members) == 0 {
		e.splice(obj.start+1, obj.end-1, quote(key)+st.kv+raw)
		return
	}
	end := obj.members[len(obj.members)-1].val.end
	e.splice(end, end, st.sep+quote(key)+st.kv+raw)
}

func (e *editor) removeMember(obj *value, i int) {
	ms := obj.members
	switch {
	case len(ms) == 1:
		e.splice(obj.start+1, obj.end-1, "")
	case i > 0:
		e.splice(ms[i-1].val.end, ms[i].val.end, "")
	default:
		e.splice(ms[0].keyStart, ms[1].keyStart, "")
	}
}

func (e *editor) removeItem(arr *value, i int) {
	it := arr.items
	switch {
	case len(it) == 1:
		e.splice(arr.start+1, arr.end-1, "")
	case i > 0:
		e.splice(it[i-1].end, it[i].end, "")
	default:
		e.splice(it[0].start, it[1].start, "")
	}
}

// appendItem appends an object to the named top-level array, creating the
// array if needed. The object copies the layout of the array's last item,
// or of the other array's, or a one-line layout.
func (e *editor) appendItem(key string, members [][2]string) error {
	if e.c.root.get(key) == nil {
		e.appendMember(e.c.root, key, "[]")
	}
	arr := e.c.root.get(key)
	if arr.kind != kArray {
		return fmt.Errorf("%q isn't an array", key)
	}
	other := e.c.root.get(map[string]string{"nodes": "edges", "edges": "nodes"}[key])
	if other != nil && (other.kind != kArray || len(other.items) == 0) {
		other = nil
	}
	src := e.c.src
	st := style{kv: e.styleOf(e.c.root).kv, sep: ","}
	if strings.ContainsAny(st.kv, "\r\n") {
		st.kv = ":"
	}
	switch {
	case len(arr.items) > 0 && arr.items[len(arr.items)-1].kind == kObject:
		st = e.styleOf(arr.items[len(arr.items)-1])
	case other != nil && other.items[len(other.items)-1].kind == kObject:
		st = e.styleOf(other.items[len(other.items)-1])
	}
	text := st.render(members)
	if n := len(arr.items); n > 0 {
		sep := "," + string(src[arr.start+1:arr.items[0].start])
		if n >= 2 {
			sep = string(src[arr.items[n-2].end:arr.items[n-1].start])
		}
		e.splice(arr.items[n-1].end, arr.items[n-1].end, sep+text)
		return nil
	}
	var prefix, suffix string
	switch {
	case other != nil:
		prefix = string(src[other.start+1 : other.items[0].start])
		suffix = string(src[other.items[len(other.items)-1].end : other.end-1])
	case strings.Contains(string(src), "\n"):
		base, eol := e.lineIndent(arr.start), e.c.eol()
		prefix, suffix = eol+base+e.unit(), eol+base
	}
	e.splice(arr.start+1, arr.end-1, prefix+text+suffix)
	return nil
}

// lineIndent returns the leading whitespace of the line holding pos.
func (e *editor) lineIndent(pos int) string {
	src := e.c.src
	ls := strings.LastIndexByte(string(src[:pos]), '\n') + 1
	j := ls
	for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
		j++
	}
	return string(src[ls:j])
}

// unit is the file's indent unit: the top-level keys' indent, or a tab.
func (e *editor) unit() string {
	if ms := e.c.root.members; len(ms) > 0 {
		if in := e.lineIndent(ms[0].keyStart); in != "" {
			return in
		}
	}
	return "\t"
}
