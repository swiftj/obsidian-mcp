// Package canvas reads, validates and edits JSON Canvas 1.0 files
// (.canvas). Edits patch the original bytes: untouched nodes and edges, key
// order, unknown keys and indentation are kept (C-PRES-01).
package canvas

import (
	"errors"
	"strconv"
	"strings"
)

// Canvas is a parsed .canvas file.
type Canvas struct {
	src  []byte
	root *value
}

// Node is a canvas node as read. Fields that are missing or of the wrong
// type are zero; Validate reports them.
type Node struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	X               int    `json:"x"`
	Y               int    `json:"y"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	Color           string `json:"color,omitempty"`
	Text            string `json:"text,omitempty"`
	File            string `json:"file,omitempty"`
	Subpath         string `json:"subpath,omitempty"`
	URL             string `json:"url,omitempty"`
	Label           string `json:"label,omitempty"`
	Background      string `json:"background,omitempty"`
	BackgroundStyle string `json:"backgroundStyle,omitempty"`
}

// Edge is a canvas edge as read.
type Edge struct {
	ID       string `json:"id"`
	FromNode string `json:"fromNode"`
	FromSide string `json:"fromSide,omitempty"`
	FromEnd  string `json:"fromEnd,omitempty"`
	ToNode   string `json:"toNode"`
	ToSide   string `json:"toSide,omitempty"`
	ToEnd    string `json:"toEnd,omitempty"`
	Color    string `json:"color,omitempty"`
	Label    string `json:"label,omitempty"`
}

// ErrNotObject reports a file whose top level isn't a JSON object.
var ErrNotObject = errors.New("canvas: the top level must be a JSON object")

// Parse parses a canvas. It fails only when the bytes aren't JSON or the
// top level isn't an object; everything else is for Validate.
func Parse(data []byte) (*Canvas, error) {
	root, err := parseJSON(data)
	if err != nil {
		return nil, err
	}
	if root.kind != kObject {
		return nil, ErrNotObject
	}
	return &Canvas{src: data, root: root}, nil
}

func (c *Canvas) array(key string) []*value {
	if a := c.root.get(key); a != nil && a.kind == kArray {
		return a.items
	}
	return nil
}

func str(v *value, key string) string {
	if f := v.get(key); f != nil && f.kind == kString {
		return f.str
	}
	return ""
}

func num(src []byte, v *value, key string) int {
	f := v.get(key)
	if f == nil || f.kind != kNumber {
		return 0
	}
	n, _ := strconv.Atoi(string(src[f.start:f.end]))
	return n
}

// Nodes returns the nodes in file (z-) order.
func (c *Canvas) Nodes() []Node {
	var out []Node
	for _, v := range c.array("nodes") {
		if v.kind != kObject {
			continue
		}
		out = append(out, Node{
			ID: str(v, "id"), Type: str(v, "type"),
			X: num(c.src, v, "x"), Y: num(c.src, v, "y"),
			Width: num(c.src, v, "width"), Height: num(c.src, v, "height"),
			Color: str(v, "color"), Text: str(v, "text"),
			File: str(v, "file"), Subpath: str(v, "subpath"), URL: str(v, "url"),
			Label: str(v, "label"), Background: str(v, "background"),
			BackgroundStyle: str(v, "backgroundStyle"),
		})
	}
	return out
}

// Edges returns the edges in file order.
func (c *Canvas) Edges() []Edge {
	var out []Edge
	for _, v := range c.array("edges") {
		if v.kind != kObject {
			continue
		}
		out = append(out, Edge{
			ID: str(v, "id"), FromNode: str(v, "fromNode"), FromSide: str(v, "fromSide"),
			FromEnd: str(v, "fromEnd"), ToNode: str(v, "toNode"), ToSide: str(v, "toSide"),
			ToEnd: str(v, "toEnd"), Color: str(v, "color"), Label: str(v, "label"),
		})
	}
	return out
}

// StringSpan returns the byte range of the string value of key in the first
// node with the given id, quotes included. The link rewriter uses it to
// patch a file node's "file" and "subpath" in place.
func (c *Canvas) StringSpan(nodeID, key string) (start, end int, ok bool) {
	for _, v := range c.array("nodes") {
		if str(v, "id") != nodeID {
			continue
		}
		f := v.get(key)
		if f == nil || f.kind != kString {
			return 0, 0, false
		}
		return f.start, f.end, true
	}
	return 0, 0, false
}

// Quote encodes s as a JSON string, without HTML escaping.
func Quote(s string) string { return quote(s) }

func (c *Canvas) eol() string {
	if strings.Contains(string(c.src), "\r\n") {
		return "\r\n"
	}
	return "\n"
}
