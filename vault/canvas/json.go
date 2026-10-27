package canvas

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// kind is a JSON value's kind.
type kind uint8

const (
	kObject kind = iota + 1
	kArray
	kString
	kNumber
	kBool
	kNull
)

// value is a parsed JSON value with its byte span in the source. Objects
// keep their members in order and arrays their items, so edits can patch
// one value and leave every other byte alone.
type value struct {
	kind       kind
	start, end int
	members    []member // objects
	items      []*value // arrays
	str        string   // strings, decoded
}

type member struct {
	key      string
	keyStart int // the opening quote of the key
	keyEnd   int // just after the closing quote
	val      *value
}

func (v *value) get(key string) *value {
	if v == nil || v.kind != kObject {
		return nil
	}
	for _, m := range v.members {
		if m.key == key {
			return m.val
		}
	}
	return nil
}

func (v *value) index(key string) int {
	for i, m := range v.members {
		if m.key == key {
			return i
		}
	}
	return -1
}

var errSyntax = errors.New("canvas: the file is not valid JSON")

// parseJSON parses src, which must be valid JSON, into spans.
func parseJSON(src []byte) (*value, error) {
	if !json.Valid(src) {
		return nil, errSyntax
	}
	p := &jparser{src: src}
	p.ws()
	v := p.value()
	if p.err != nil {
		return nil, p.err
	}
	return v, nil
}

type jparser struct {
	src []byte
	i   int
	err error
}

func (p *jparser) ws() {
	for p.i < len(p.src) {
		switch p.src[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

// value parses one value at p.i. The input is known to be valid JSON, so
// only the shape matters here.
func (p *jparser) value() *value {
	if p.err != nil || p.i >= len(p.src) {
		p.err = errSyntax
		return &value{}
	}
	v := &value{start: p.i}
	switch c := p.src[p.i]; c {
	case '{':
		v.kind = kObject
		p.i++
		p.ws()
		for p.err == nil && p.i < len(p.src) && p.src[p.i] != '}' {
			ks := p.i
			k := p.value()
			if k.kind != kString {
				p.err = errSyntax
			}
			p.ws()
			p.i++ // ':'
			p.ws()
			v.members = append(v.members, member{key: k.str, keyStart: ks, keyEnd: k.end, val: p.value()})
			p.ws()
			if p.i < len(p.src) && p.src[p.i] == ',' {
				p.i++
				p.ws()
			}
		}
		p.i++
	case '[':
		v.kind = kArray
		p.i++
		p.ws()
		for p.err == nil && p.i < len(p.src) && p.src[p.i] != ']' {
			v.items = append(v.items, p.value())
			p.ws()
			if p.i < len(p.src) && p.src[p.i] == ',' {
				p.i++
				p.ws()
			}
		}
		p.i++
	case '"':
		v.kind = kString
		j := p.i + 1
		for j < len(p.src) && p.src[j] != '"' {
			if p.src[j] == '\\' {
				j++
			}
			j++
		}
		p.i = j + 1
		if json.Unmarshal(p.src[v.start:p.i], &v.str) != nil {
			p.err = errSyntax
		}
	case 't', 'f':
		v.kind = kBool
		p.i += map[byte]int{'t': 4, 'f': 5}[c]
	case 'n':
		v.kind = kNull
		p.i += 4
	default:
		v.kind = kNumber
		for p.i < len(p.src) && strings.IndexByte("+-0123456789.eE", p.src[p.i]) >= 0 {
			p.i++
		}
	}
	v.end = p.i
	return v
}

// quote encodes s as a JSON string without HTML escaping.
func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
