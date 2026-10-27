package canvas

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const tabbed = "{\n\t\"nodes\":[\n" +
	"\t\t{\"id\":\"t1\",\"type\":\"text\",\"text\":\"Hello\",\"x\":0,\"y\":0,\"width\":250,\"height\":60,\"plugin\":{\"k\":1}},\n" +
	"\t\t{\"id\":\"f1\",\"type\":\"file\",\"file\":\"Notes/A.md\",\"subpath\":\"#Intro\",\"x\":300,\"y\":0,\"width\":400,\"height\":400,\"color\":\"4\"},\n" +
	"\t\t{\"id\":\"l1\",\"type\":\"link\",\"url\":\"https://example.com\",\"x\":0,\"y\":500,\"width\":400,\"height\":300},\n" +
	"\t\t{\"id\":\"g1\",\"type\":\"group\",\"label\":\"Box\",\"backgroundStyle\":\"cover\",\"x\":-50,\"y\":-50,\"width\":900,\"height\":900,\"color\":\"#FF0000\"}\n" +
	"\t],\n\t\"edges\":[\n" +
	"\t\t{\"id\":\"e1\",\"fromNode\":\"t1\",\"fromSide\":\"right\",\"toNode\":\"f1\",\"toSide\":\"left\",\"toEnd\":\"arrow\",\"label\":\"see\"}\n" +
	"\t],\n\t\"extra\":true\n}"

const spaced = `{
  "nodes": [
    {
      "id": "a",
      "type": "text",
      "text": "A",
      "x": 0,
      "y": 0,
      "width": 10,
      "height": 10
    }
  ],
  "edges": []
}
`

func mustApply(t *testing.T, src string, ops ...Op) *Result {
	t.Helper()
	r, err := Apply([]byte(src), ops)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := Parse(r.Data); err != nil {
		t.Fatalf("result doesn't parse: %v\n%s", err, r.Data)
	}
	return r
}

func TestReadAndValidate(t *testing.T) {
	// C-SCH-01, C-NODE-01, C-NODE-03, C-EDGE-01, C-COL-01
	c, err := Parse([]byte(tabbed))
	if err != nil {
		t.Fatal(err)
	}
	if ps := c.Validate(); len(ps) != 0 {
		t.Fatalf("spec example has problems: %v", ps)
	}
	ns := c.Nodes()
	if len(ns) != 4 || ns[1].File != "Notes/A.md" || ns[1].Subpath != "#Intro" || ns[3].X != -50 || ns[3].BackgroundStyle != "cover" {
		t.Errorf("Nodes = %+v", ns)
	}
	if es := c.Edges(); len(es) != 1 || es[0].FromNode != "t1" || es[0].Label != "see" {
		t.Errorf("Edges = %+v", c.Edges())
	}
	for _, src := range []string{`{}`, `{"nodes":[]}`, `{"edges":[]}`} {
		c, _ := Parse([]byte(src))
		if ps := c.Validate(); len(ps) != 0 {
			t.Errorf("C-SCH-01 %s: %v", src, ps)
		}
	}
	if _, err := Parse([]byte(`[]`)); !errors.Is(err, ErrNotObject) {
		t.Errorf("C-SCH-01 array top level: %v", err)
	}
	if _, err := Parse([]byte(`{"nodes":`)); err == nil {
		t.Error("broken JSON parsed")
	}
}

func TestValidateProblems(t *testing.T) {
	// C-SCH-01, C-NODE-01, C-NODE-02, C-NODE-03, C-EDGE-01, C-COL-01
	node := func(extra string) string {
		return `{"nodes":[{"id":"n","type":"text","text":"x","x":0,"y":0,"width":1,"height":1` + extra + `}]}`
	}
	cases := []struct{ src, want string }{
		{`{"nodes":{}}`, `canvas: "nodes" must be an array`},
		{`{"edges":"x"}`, `canvas: "edges" must be an array`},
		{`{"nodes":[{"type":"text","text":"x","x":0,"y":0,"width":1,"height":1}]}`, `nodes[0]: "id" must be a non-empty string`},
		{`{"nodes":[{"id":"n","type":"text","text":"x","x":1.5,"y":0,"width":1,"height":1}]}`, `node "n": "x" must be an integer`},
		{`{"nodes":[{"id":"n","type":"text","text":"x","x":0,"y":0,"width":1}]}`, `node "n": "height" must be an integer`},
		{`{"nodes":[{"id":"n","type":"card","x":0,"y":0,"width":1,"height":1}]}`, `node "n": "type" must be one of [text file link group], not "card"`},
		{`{"nodes":[{"id":"n","type":"text","x":0,"y":0,"width":1,"height":1}]}`, `node "n": "text" is required`},
		{`{"nodes":[{"id":"n","type":"file","x":0,"y":0,"width":1,"height":1}]}`, `node "n": "file" is required`},
		{`{"nodes":[{"id":"n","type":"file","file":"a.md","subpath":"Intro","x":0,"y":0,"width":1,"height":1}]}`, `node "n": "subpath" must start with "#"`},
		{`{"nodes":[{"id":"n","type":"link","x":0,"y":0,"width":1,"height":1}]}`, `node "n": "url" is required`},
		{`{"nodes":[{"id":"n","type":"group","backgroundStyle":"tile","x":0,"y":0,"width":1,"height":1}]}`, `node "n": "backgroundStyle" must be one of [cover ratio repeat], not "tile"`},
		{`{"nodes":[{"id":"n","type":"group","label":3,"x":0,"y":0,"width":1,"height":1}]}`, `node "n": "label" must be a string`},
		{node(`,"color":"7"`), `node "n": "color" must be a preset`},
		{node(`,"color":"red"`), `node "n": "color" must be a preset`},
		{node(`,"color":"#F00"`), `node "n": "color" must be a preset`},
		{`{"nodes":[{"id":"n","type":"text","text":"x","x":0,"y":0,"width":1,"height":1},{"id":"n","type":"text","text":"y","x":0,"y":0,"width":1,"height":1}]}`, `node "n": id "n" is used more than once`},
		{`{"nodes":[{"id":"n","type":"text","text":"x","x":0,"y":0,"width":1,"height":1}],"edges":[{"id":"n","fromNode":"n","toNode":"n"}]}`, `edge "n": id "n" is used more than once`},
		{`{"nodes":[],"edges":[{"id":"e","fromNode":"a","toNode":"b"}]}`, `edge "e": "fromNode" names no node: "a"`},
		{`{"edges":[{"id":"e","toNode":"b"}]}`, `edge "e": "fromNode" is required`},
		{node(``)[:len(node(``))-2] + `]` + `,"edges":[{"id":"e","fromNode":"n","toNode":"n","fromSide":"up"}]}`, `edge "e": "fromSide" must be one of [top right bottom left], not "up"`},
		{node(``)[:len(node(``))-2] + `]` + `,"edges":[{"id":"e","fromNode":"n","toNode":"n","toEnd":"dot"}]}`, `edge "e": "toEnd" must be one of [none arrow], not "dot"`},
		{`{"nodes":[3]}`, `nodes[0]: a node must be an object`},
	}
	for _, tc := range cases {
		c, err := Parse([]byte(tc.src))
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		var got []string
		for _, p := range c.Validate() {
			got = append(got, p.String())
		}
		if !slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, tc.want) }) {
			t.Errorf("%s:\n got %q\nwant %q", tc.src, got, tc.want)
		}
	}
	for _, ok := range []string{"1", "6", "#00ff00", "#ABCDEF"} {
		if !ValidColor(ok) {
			t.Errorf("C-COL-01 ValidColor(%q) = false", ok)
		}
	}
}

func TestApplyPreservesBytes(t *testing.T) {
	// C-PRES-01, C-ORD-01, C-EDIT-01
	r := mustApply(t, tabbed, Op{Kind: UpdateNode, ID: "f1", Set: map[string]any{"x": 310, "label": "x"}, Unset: []string{"color"}})
	want := strings.Replace(tabbed,
		`{"id":"f1","type":"file","file":"Notes/A.md","subpath":"#Intro","x":300,"y":0,"width":400,"height":400,"color":"4"}`,
		`{"id":"f1","type":"file","file":"Notes/A.md","subpath":"#Intro","x":310,"y":0,"width":400,"height":400,"label":"x"}`, 1)
	if string(r.Data) != want {
		t.Errorf("C-PRES-01 update:\n%s", r.Data)
	}

	r = mustApply(t, tabbed, Op{Kind: AddNode, ID: "t2", Set: map[string]any{"type": "text", "text": "New <b>", "x": 1, "y": 2, "width": 3, "height": 4}})
	want = strings.Replace(tabbed, "\"color\":\"#FF0000\"}\n\t],",
		"\"color\":\"#FF0000\"},\n\t\t{\"id\":\"t2\",\"type\":\"text\",\"x\":1,\"y\":2,\"width\":3,\"height\":4,\"text\":\"New <b>\"}\n\t],", 1)
	if string(r.Data) != want {
		t.Errorf("C-ORD-01 append:\n%s", r.Data)
	}

	r = mustApply(t, tabbed, Op{Kind: RemoveNode, ID: "t1"})
	if strings.Contains(string(r.Data), `"t1"`) || len(r.RemovedEdges) != 1 || r.RemovedEdges[0] != "e1" {
		t.Errorf("C-EDIT-01 remove cascades: %v\n%s", r.RemovedEdges, r.Data)
	}
	if !strings.Contains(string(r.Data), "\"nodes\":[\n\t\t{\"id\":\"f1\"") || !strings.Contains(string(r.Data), "\"edges\":[],\n\t\"extra\":true") {
		t.Errorf("C-PRES-01 removal layout:\n%s", r.Data)
	}
	// Removing the last edge and adding it back restores the file.
	r = mustApply(t, tabbed, Op{Kind: RemoveEdge, ID: "e1"},
		Op{Kind: AddEdge, ID: "e1", Set: map[string]any{"fromNode": "t1", "fromSide": "right", "toNode": "f1", "toSide": "left", "toEnd": "arrow", "label": "see"}})
	if string(r.Data) != tabbed {
		t.Errorf("C-PRES-01 edge round trip:\n%s", r.Data)
	}
	if r, _ := Apply([]byte(tabbed), nil); string(r.Data) != tabbed {
		t.Error("C-PRES-01 no ops changed the file")
	}
}

func TestApplyStyles(t *testing.T) {
	// C-PRES-01
	r := mustApply(t, spaced, Op{Kind: UpdateNode, ID: "a", Set: map[string]any{"color": "2"}},
		Op{Kind: AddNode, ID: "b", Set: map[string]any{"type": "link", "url": "u", "x": 0, "y": 0, "width": 1, "height": 1}},
		Op{Kind: AddEdge, ID: "e", Set: map[string]any{"fromNode": "a", "toNode": "b"}})
	want := `{
  "nodes": [
    {
      "id": "a",
      "type": "text",
      "text": "A",
      "x": 0,
      "y": 0,
      "width": 10,
      "height": 10,
      "color": "2"
    },
    {
      "id": "b",
      "type": "link",
      "x": 0,
      "y": 0,
      "width": 1,
      "height": 1,
      "url": "u"
    }
  ],
  "edges": [
    {
      "id": "e",
      "fromNode": "a",
      "toNode": "b"
    }
  ]
}
`
	if string(r.Data) != want {
		t.Errorf("2-space style:\n%s", r.Data)
	}
	r = mustApply(t, `{"nodes":[],"edges":[]}`, Op{Kind: AddNode, ID: "a", Set: map[string]any{"type": "text", "text": "t", "x": 0, "y": 0, "width": 1, "height": 1}})
	if string(r.Data) != `{"nodes":[{"id":"a","type":"text","x":0,"y":0,"width":1,"height":1,"text":"t"}],"edges":[]}` {
		t.Errorf("compact style: %s", r.Data)
	}
	r = mustApply(t, "{\r\n\t\"extra\":1\r\n}", Op{Kind: AddNode, ID: "a", Set: map[string]any{"type": "text", "text": "t", "x": 0, "y": 0, "width": 1, "height": 1}})
	if string(r.Data) != "{\r\n\t\"extra\":1,\r\n\t\"nodes\":[\r\n\t\t{\"id\":\"a\",\"type\":\"text\",\"x\":0,\"y\":0,\"width\":1,\"height\":1,\"text\":\"t\"}\r\n\t]\r\n}" {
		t.Errorf("created array, CRLF: %q", r.Data)
	}
	n, err := New([]Op{{Kind: AddNode, ID: "a", Set: map[string]any{"type": "text", "text": "t", "x": 0, "y": 0, "width": 1, "height": 1}}})
	if err != nil || string(n.Data) != "{\n\t\"nodes\":[\n\t\t{\"id\":\"a\",\"type\":\"text\",\"x\":0,\"y\":0,\"width\":1,\"height\":1,\"text\":\"t\"}\n\t],\n\t\"edges\":[]\n}" {
		t.Errorf("C-PRES-01 new file uses tabs: %v\n%s", err, n.Data)
	}
}

func TestApplyIDsAndErrors(t *testing.T) {
	// C-NODE-02, C-EDIT-01
	text := map[string]any{"type": "text", "text": "t", "x": 0, "y": 0, "width": 1, "height": 1}
	r := mustApply(t, tabbed, Op{Kind: AddNode, Set: text}, Op{Kind: AddNode, Set: text})
	hexRE := regexp.MustCompile(`^[0-9a-f]{16}$`)
	if len(r.Added) != 2 || r.Added[0] == r.Added[1] || !hexRE.MatchString(r.Added[0]) {
		t.Errorf("C-NODE-02 generated IDs = %v", r.Added)
	}
	refused := []struct {
		op   Op
		want string
	}{
		{Op{Kind: AddNode, ID: "e1", Set: text}, `id "e1" is already used`},
		{Op{Kind: UpdateNode, ID: "nope", Set: map[string]any{"x": 1}}, `no node has id "nope"`},
		{Op{Kind: UpdateNode, ID: "t1", Set: map[string]any{"id": "z"}}, `IDs can't be changed`},
		{Op{Kind: UpdateNode, ID: "t1", Set: map[string]any{"x": 1.5}}, `"x" must be an integer`},
		{Op{Kind: UpdateNode, ID: "t1", Set: map[string]any{"text": 3}}, `"text" must be a string`},
		{Op{Kind: UpdateNode, ID: "t1", Set: map[string]any{"bogus": "1"}}, `"bogus" is not a node field`},
		{Op{Kind: UpdateNode, ID: "t1", Set: map[string]any{"color": "9"}}, `"color" must be a preset`},
		{Op{Kind: UpdateNode, ID: "t1", Unset: []string{"text"}}, `"text" is required`},
		{Op{Kind: AddEdge, ID: "e2", Set: map[string]any{"fromNode": "t1", "toNode": "zz"}}, `"toNode" names no node`},
		{Op{Kind: RemoveEdge, ID: "t1"}, `no edge has id "t1"`},
		{Op{Kind: "paint"}, `unknown op`},
	}
	for _, tc := range refused {
		_, err := Apply([]byte(tabbed), []Op{tc.op})
		var ce *Error
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: err = %v; want %q", tc.op, err, tc.want)
		}
	}
	// Problems already in the file don't block an unrelated edit (D-013).
	bad := `{"nodes":[{"id":"a","type":"text","text":"t","x":0.5,"y":0,"width":1,"height":1},{"id":"b","type":"text","text":"t","x":0,"y":0,"width":1,"height":1}]}`
	r = mustApply(t, bad, Op{Kind: UpdateNode, ID: "b", Set: map[string]any{"text": "u"}})
	if len(r.Existing) != 1 || r.Existing[0].Where != `node "a"` {
		t.Errorf("existing problems = %v", r.Existing)
	}
}

func TestStringSpan(t *testing.T) {
	c, _ := Parse([]byte(tabbed))
	s, e, ok := c.StringSpan("f1", "subpath")
	if !ok || tabbed[s:e] != `"#Intro"` {
		t.Errorf("StringSpan = %q %v", tabbed[s:e], ok)
	}
	if _, _, ok := c.StringSpan("f1", "x"); ok {
		t.Error("StringSpan of a number")
	}
}

func FuzzApply(f *testing.F) {
	for _, s := range []string{tabbed, spaced, `{}`, `{"nodes":[]}`, `{"nodes":[{"id":"a"}],"edges":[1]}`, `{"a":"\u00e9\"","nodes":[]}`} {
		f.Add(s, "x", 5)
	}
	f.Fuzz(func(t *testing.T, src, text string, x int) {
		c, err := Parse([]byte(src))
		if err != nil {
			return
		}
		c.Validate()
		c.Nodes()
		c.Edges()
		r, err := Apply([]byte(src), nil)
		if err != nil || string(r.Data) != src {
			t.Fatalf("no-op Apply changed the file: %v", err)
		}
		ops := []Op{{Kind: AddNode, ID: "fz", Set: map[string]any{"type": "text", "text": text, "x": x, "y": 0, "width": 1, "height": 1}}}
		if r, err := Apply([]byte(src), ops); err == nil {
			c2, err := Parse(r.Data)
			if err != nil {
				t.Fatalf("result doesn't parse: %v", err)
			}
			if i := slices.IndexFunc(c2.Nodes(), func(n Node) bool { return n.ID == "fz" }); i < 0 || c2.Nodes()[i].Text != text {
				t.Fatalf("added node not read back:\n%s", r.Data)
			}
		}
	})
}
