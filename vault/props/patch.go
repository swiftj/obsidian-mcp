package props

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/swiftj/obsidian-mcp/vault/ofm"
	"go.yaml.in/yaml/v3"
)

// Error reports a property write that was refused.
type Error struct {
	Name   string
	Reason string
	Hint   string
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("props: %s: %s", strconv.Quote(e.Name), e.Reason)
	if e.Hint != "" {
		msg += " (" + e.Hint + ")"
	}
	return msg
}

// SetOptions modifies Set.
type SetOptions struct {
	// Types holds the vault's recorded types (types.json).
	Types Types
	// As is the type to write when the name has no recorded or built-in
	// type. "" infers it from the value.
	As Type
}

// Set sets property name to value and returns the patched note. Only the
// lines of that property change (P-FM-01). value may be a string, number,
// bool, nil (an empty value), or a list of those.
func Set(src []byte, name string, value any, o SetOptions) ([]byte, error) {
	name = strings.TrimSpace(name)
	if err := checkKey(name); err != nil {
		return nil, err
	}
	canon := Canonical(name)
	if !validUTF8(value) {
		return nil, &Error{Name: canon, Reason: "the value is not valid UTF-8"}
	}
	d := ofm.Parse(src)
	f := Read(d)
	if err := f.writable(canon); err != nil {
		return nil, err
	}
	t := o.Types[canon]
	if t == "" {
		t = builtinTypes[canon]
	}
	if t == "" {
		t = o.As
	}
	if t == Tags && canon != "tags" {
		return nil, &Error{Name: canon, Reason: "only the tags property can have the Tags type", Hint: "use List (multitext) instead"}
	}
	existing := f.matches(canon)
	if len(existing) > 1 {
		return nil, dupError(canon)
	}
	if len(existing) == 1 && existing[0].node.Kind == yaml.MappingNode {
		return nil, &Error{Name: canon, Reason: "the current value is nested, and is left as it is", Hint: "Obsidian doesn't support nested properties; edit the note text instead"}
	}
	if t == "" {
		t = typeOfValue(value)
	}
	v, err := render(canon, value, t, o.Types[canon] != "")
	if err != nil {
		return nil, err
	}
	eol := f.eol()
	var out []byte
	switch {
	case len(existing) == 1:
		p := existing[0]
		out = splice(src, p.Lines, v.entry(renderKey(canon), eol, f.listStyle(p)))
	case f.Present:
		at := d.Frontmatter.Body.End
		out = splice(src, ofm.Span{Start: at, End: at}, v.entry(renderKey(canon), eol, listStyle{prefix: "  - "}))
	default:
		at := d.BodyStart()
		out = splice(src, ofm.Span{Start: at, End: at}, "---"+eol+v.entry(renderKey(canon), eol, listStyle{prefix: "  - "})+"---"+eol)
	}
	return verify(f, out, canon, canon)
}

// Remove deletes property name, and returns src unchanged if it is absent.
func Remove(src []byte, name string) ([]byte, error) {
	canon := Canonical(strings.TrimSpace(name))
	f := Read(ofm.Parse(src))
	if !f.Present {
		return src, nil
	}
	if err := f.writable(canon); err != nil {
		return nil, err
	}
	existing := f.matches(canon)
	switch len(existing) {
	case 0:
		return src, nil
	case 1:
		return verify(f, splice(src, existing[0].Lines, ""), canon, "")
	}
	return nil, dupError(canon)
}

// Rename renames property from to to, keeping its value's bytes. It fails
// if to already exists (P-KEY-01).
func Rename(src []byte, from, to string) ([]byte, error) {
	from, to = Canonical(strings.TrimSpace(from)), strings.TrimSpace(to)
	if err := checkKey(to); err != nil {
		return nil, err
	}
	to = Canonical(to)
	f := Read(ofm.Parse(src))
	if err := f.writable(from); err != nil {
		return nil, err
	}
	existing := f.matches(from)
	if len(existing) == 0 {
		return nil, &Error{Name: from, Reason: "no such property"}
	}
	if len(existing) > 1 {
		return nil, dupError(from)
	}
	if from == to && existing[0].Key == to {
		return src, nil
	}
	if from != to && len(f.matches(to)) > 0 {
		return nil, &Error{Name: to, Reason: "already exists", Hint: "remove or rename it first"}
	}
	k := existing[0].key
	start := f.offsetOf(k.Line, k.Column)
	end := keyEnd(src, start, k.Style)
	if end < 0 {
		return nil, &Error{Name: from, Reason: "can't locate the key in the source"}
	}
	out, err := verify(f, splice(src, ofm.Span{Start: start, End: end}, renderKey(to)), from, to)
	if err != nil {
		return nil, err
	}
	if q := Read(ofm.Parse(out)).Get(to); q == nil || q.Raw != existing[0].Raw {
		return nil, &Error{Name: from, Reason: "the rename can't be done as a minimal patch", Hint: "edit the note text instead"}
	}
	return out, nil
}

// verify re-reads a patched note and refuses it unless every property other
// than changed keeps its bytes and value, and want ("" for a removal) is
// present exactly once (P-FM-01, P-FM-02).
func verify(before *Frontmatter, out []byte, changed, want string) ([]byte, error) {
	fail := &Error{Name: changed, Reason: "this frontmatter can't be patched safely", Hint: "edit the note text instead"}
	after := Read(ofm.Parse(out))
	if after.Err != nil || len(after.Duplicates) > len(before.Duplicates) {
		return nil, fail
	}
	n := 0
	for _, p := range before.Props {
		if p.Name == changed {
			continue
		}
		n++
		if slices.Contains(before.Duplicates, p.Name) {
			continue
		}
		q := after.Get(p.Name)
		if q == nil || q.Raw != p.Raw || !reflect.DeepEqual(q.Value, p.Value) ||
			string(out[q.Lines.Start:q.Lines.End]) != string(before.doc.Src[p.Lines.Start:p.Lines.End]) {
			return nil, fail
		}
	}
	if want != "" {
		n++
		if len(after.matches(want)) != 1 {
			return nil, fail
		}
	}
	if len(after.Props) != n {
		return nil, fail
	}
	return out, nil
}

func (f *Frontmatter) writable(name string) error {
	switch {
	case f.JSON:
		return &Error{Name: name, Reason: "JSON frontmatter is read-only", Hint: "a minimal patch isn't possible, and Obsidian re-saves JSON properties as YAML; convert the note's properties to YAML first"}
	case f.Err != nil:
		return &Error{Name: name, Reason: "the frontmatter can't be parsed: " + f.Err.Error(), Hint: "fix the YAML first"}
	case f.flow:
		return &Error{Name: name, Reason: "the frontmatter is a single flow-style mapping", Hint: "rewrite it in block style first"}
	case f.unpatchable != "":
		return &Error{Name: name, Reason: f.unpatchable, Hint: "edit the note text instead"}
	}
	return nil
}

func (f *Frontmatter) matches(canon string) []Property {
	var out []Property
	for _, p := range f.Props {
		if p.Name == canon {
			out = append(out, p)
		}
	}
	return out
}

func validUTF8(v any) bool {
	switch x := v.(type) {
	case string:
		return utf8.ValidString(x)
	case []string:
		for _, s := range x {
			if !utf8.ValidString(s) {
				return false
			}
		}
	case []any:
		for _, e := range x {
			if !validUTF8(e) {
				return false
			}
		}
	}
	return true
}

func dupError(name string) error {
	return &Error{Name: name, Reason: "appears more than once in the frontmatter", Hint: "remove the duplicate keys first"}
}

// maxKeyLen keeps keys well inside YAML's 1024-character limit on implicit keys.
const maxKeyLen = 256

func checkKey(name string) error {
	if name == "" || len(name) > maxKeyLen || !utf8.ValidString(name) || strings.HasPrefix(name, "#") ||
		strings.IndexFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return &Error{Name: name, Reason: "not a valid property name"}
	}
	return nil
}

// eol returns the line ending the frontmatter (or the note) uses.
func (f *Frontmatter) eol() string {
	src := f.doc.Src
	if i := bytes.IndexByte(src, '\n'); i > 0 && src[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

func splice(src []byte, s ofm.Span, text string) []byte {
	out := make([]byte, 0, len(src)-s.Len()+len(text))
	out = append(out, src[:s.Start]...)
	out = append(out, text...)
	return append(out, src[s.End:]...)
}

// keyEnd returns the offset just past a key token that starts at i.
func keyEnd(src []byte, i int, style yaml.Style) int {
	switch {
	case style&yaml.DoubleQuotedStyle != 0:
		for j := i + 1; j < len(src) && src[j] != '\n'; j++ {
			switch src[j] {
			case '\\':
				j++
			case '"':
				return j + 1
			}
		}
		return -1
	case style&yaml.SingleQuotedStyle != 0:
		for j := i + 1; j < len(src) && src[j] != '\n'; j++ {
			if src[j] == '\'' {
				if j+1 < len(src) && src[j+1] == '\'' {
					j++
					continue
				}
				return j + 1
			}
		}
		return -1
	}
	for j := i; j < len(src) && src[j] != '\n'; j++ {
		if src[j] == ':' && (j+1 == len(src) || strings.IndexByte(" \t\r\n", src[j+1]) >= 0) {
			end := j
			for end > i && (src[end-1] == ' ' || src[end-1] == '\t') {
				end--
			}
			return end
		}
	}
	return -1
}

// --- rendering ---

type listStyle struct {
	flow   bool
	prefix string // block item prefix, such as "  - "
}

// listStyle returns how an existing list is written, so a replacement keeps
// it; new lists use "  - ", as Obsidian writes them.
func (f *Frontmatter) listStyle(p Property) listStyle {
	n := p.node
	if n.Kind != yaml.SequenceNode {
		return listStyle{prefix: "  - "}
	}
	if n.Style&yaml.FlowStyle != 0 {
		return listStyle{flow: true}
	}
	if len(n.Content) == 0 {
		return listStyle{prefix: "  - "}
	}
	first := n.Content[0]
	ls := f.lineAt(first.Line)
	at := f.offsetOf(first.Line, first.Column)
	pre := string(f.doc.Src[ls:at])
	if strings.TrimSpace(pre) != "-" {
		return listStyle{prefix: "  - "}
	}
	return listStyle{prefix: pre}
}

type rendered struct {
	scalar string
	list   []string
	isList bool
}

func (r rendered) entry(key, eol string, ls listStyle) string {
	if !r.isList {
		if r.scalar == "" {
			return key + ":" + eol
		}
		return key + ": " + r.scalar + eol
	}
	if len(r.list) == 0 {
		return key + ": []" + eol
	}
	if ls.flow {
		items := make([]string, len(r.list))
		for i, it := range r.list {
			if !strings.HasPrefix(it, "\"") && !strings.HasPrefix(it, "'") && strings.ContainsAny(it, ",[]{}#") {
				it = quote(it)
			}
			items[i] = it
		}
		return key + ": [" + strings.Join(items, ", ") + "]" + eol
	}
	var sb strings.Builder
	sb.WriteString(key + ":" + eol)
	for _, it := range r.list {
		sb.WriteString(ls.prefix + it + eol)
	}
	return sb.String()
}

var numberRE = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

// typeOfValue infers the type to write for a value with no recorded type.
func typeOfValue(v any) Type {
	switch x := v.(type) {
	case nil:
		return Text
	case bool:
		return Checkbox
	case int, int64, float64, json.Number:
		return Number
	case string:
		return inferString(x)
	case []string, []any:
		return Multitext
	}
	return Unknown
}

// render validates value against type t and renders it as YAML
// (P-TYPE-02..07). recorded says whether t came from types.json, for the
// error hint.
func render(name string, value any, t Type, recorded bool) (rendered, error) {
	bad := func(want string) (rendered, error) {
		reason := fmt.Sprintf("%v is not %s", describe(value), want)
		if recorded {
			reason = fmt.Sprintf("%s is a %s property, and %v is not %s", name, t, describe(value), want)
		}
		return rendered{}, &Error{Name: name, Reason: reason, Hint: hintFor(t)}
	}
	if _, isMap := value.(map[string]any); isMap {
		return rendered{}, &Error{Name: name, Reason: "nested values are not supported", Hint: "Obsidian properties can't be nested"}
	}
	switch t {
	case Number:
		switch x := value.(type) {
		case int:
			return rendered{scalar: strconv.Itoa(x)}, nil
		case int64:
			return rendered{scalar: strconv.FormatInt(x, 10)}, nil
		case float64:
			if math.IsInf(x, 0) || math.IsNaN(x) {
				return bad("a number")
			}
			return rendered{scalar: strconv.FormatFloat(x, 'f', -1, 64)}, nil
		case json.Number:
			if numberRE.MatchString(x.String()) {
				return rendered{scalar: x.String()}, nil
			}
		case string:
			if numberRE.MatchString(x) {
				return rendered{scalar: x}, nil
			}
		}
		return bad("a number")
	case Checkbox:
		switch x := value.(type) {
		case nil:
			return rendered{}, nil // indeterminate (P-TYPE-03)
		case bool:
			return rendered{scalar: strconv.FormatBool(x)}, nil
		case string:
			if x == "true" || x == "false" {
				return rendered{scalar: x}, nil
			}
		}
		return bad("true or false")
	case Date:
		if s, ok := value.(string); ok && dateRE.MatchString(s) {
			if _, err := time.Parse("2006-01-02", s); err == nil {
				return rendered{scalar: s}, nil
			}
		}
		if value == nil {
			return rendered{}, nil
		}
		return bad("a date (YYYY-MM-DD)")
	case Datetime:
		if s, ok := value.(string); ok {
			if datetimeRE.MatchString(s) {
				s += ":00" // written with seconds, as Obsidian's picker does
			}
			if _, err := time.Parse("2006-01-02T15:04:05", s); err == nil && len(s) == 19 {
				return rendered{scalar: s}, nil
			}
		}
		if value == nil {
			return rendered{}, nil
		}
		return bad("a date and time (YYYY-MM-DDTHH:mm:ss)")
	case Text:
		switch x := value.(type) {
		case nil:
			return rendered{}, nil
		case string:
			return rendered{scalar: renderString(x)}, nil
		case bool, int, int64, float64, json.Number:
			return rendered{scalar: renderString(fmt.Sprint(x))}, nil
		}
		return bad("text")
	case Multitext, Aliases, Tags:
		items, ok := listItems(value)
		if !ok {
			return bad("a list of text values")
		}
		out := rendered{isList: true}
		for _, it := range items {
			if t == Tags {
				it = strings.TrimPrefix(it, "#")
				if !validTag(it) {
					return rendered{}, &Error{Name: name, Reason: fmt.Sprintf("%q is not a valid tag", it), Hint: hintFor(Tags)}
				}
			}
			out.list = append(out.list, renderString(it))
		}
		return out, nil
	}
	return rendered{}, &Error{Name: name, Reason: fmt.Sprintf("properties of type %q can't be written", t)}
}

func listItems(v any) ([]string, bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case string:
		return []string{x}, true
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s := scalarString(e)
			if _, isStr := e.(string); !isStr && s == "" {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func validTag(name string) bool {
	d := ofm.Parse([]byte("#" + name))
	return len(d.Tags) == 1 && d.Tags[0].Name == name
}

func describe(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprintf("%v", v)
}

func hintFor(t Type) string {
	switch t {
	case Number:
		return "use an integer or decimal, such as 42 or 3.5"
	case Checkbox:
		return "use true or false"
	case Date:
		return "use YYYY-MM-DD, such as 2024-01-15"
	case Datetime:
		return "use YYYY-MM-DDTHH:mm:ss, such as 2024-01-15T10:30:00"
	case Tags:
		return "tags use letters, digits, _, - and /, with at least one non-digit"
	case Multitext, Aliases:
		return "use a list of text values"
	}
	return ""
}

// renderString renders a string as a YAML scalar: plain when that reads
// back as the same string, otherwise quoted. Values with links or line
// breaks are always double-quoted (P-LINK-01).
func renderString(s string) string {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	if strings.Contains(s, "[[") || strings.ContainsAny(s, "\r\n") {
		n.Style = yaml.DoubleQuotedStyle
	}
	out, err := yaml.Marshal(n)
	if err != nil {
		return quote(s)
	}
	r := strings.TrimSuffix(string(out), "\n")
	if strings.ContainsAny(r, "\n") || strings.HasPrefix(r, "|") || strings.HasPrefix(r, ">") {
		return quote(s)
	}
	return r
}

func renderKey(k string) string { return renderString(k) }

func quote(s string) string {
	out, err := yaml.Marshal(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s, Style: yaml.DoubleQuotedStyle})
	if err != nil {
		return strconv.Quote(s)
	}
	return strings.TrimSuffix(string(out), "\n")
}
