package yamledit

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// example is the base from bases-syntax.md, with comments added.
const example = `# My base
filters:
  or:
    - file.hasTag("tag")
    - and:
        - file.hasTag("book")
        - file.hasLink("Textbook")
formulas:
  formatted_price: 'if(price, price.toFixed(2) + " dollars")'
  ppu: "(price / age).toFixed(2)" # per unit
properties:
  status:
    displayName: Status
summaries:
  customAverage: 'values.mean().round(3)'
views:
  - type: table
    name: "My table"
    limit: 10
    order: [file.name, file.ext]
    summaries:
      formula.ppu: Average

  # The board
  - type: kanban
    name: Board
`

func run(t *testing.T, f func([]byte) ([]byte, error), src, want string) {
	t.Helper()
	got, err := f([]byte(src))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func set(path []any, v any) func([]byte) ([]byte, error) {
	return func(b []byte) ([]byte, error) { return Set(b, path, v, Options{KeyOrder: []string{"type", "name"}}) }
}

func TestSet(t *testing.T) {
	// B-EDIT-01
	run(t, set([]any{"formulas", "ppu"}, "price / age"), example,
		strings.Replace(example, `  ppu: "(price / age).toFixed(2)" # per unit`, `  ppu: price / age`, 1))
	run(t, set([]any{"formulas", "total"}, "price * qty"), example,
		strings.Replace(example, "# per unit\n", "# per unit\n  total: price * qty\n", 1))
	run(t, set([]any{"views", 0, "limit"}, 25), example, strings.Replace(example, "limit: 10", "limit: 25", 1))
	run(t, set([]any{"views", 1, "groupBy"}, map[string]any{"property": "note.status", "direction": "ASC"}), example,
		example+"    groupBy:\n      direction: ASC\n      property: note.status\n")
	// A flow list is rewritten as a whole entry, keeping its style.
	run(t, set([]any{"views", 0, "order", 1}, "note.age"), example, strings.Replace(example, "[file.name, file.ext]", "[file.name, note.age]", 1))
	run(t, set([]any{"views", 0, "order", 2}, "note.age"), example, strings.Replace(example, "[file.name, file.ext]", "[file.name, file.ext, note.age]", 1))
	// Missing keys are created; a value that needs quotes gets them.
	run(t, set([]any{"properties", "formula.ppu", "displayName"}, "true"), example,
		strings.Replace(example, "    displayName: Status\n", "    displayName: Status\n  formula.ppu:\n    displayName: \"true\"\n", 1))
	run(t, set([]any{"views", 2}, map[string]any{"name": "Cards", "type": "cards"}), example,
		example+"  - type: cards\n    name: Cards\n")
	// A new top-level key is appended; an empty file gets one.
	run(t, set([]any{"extra"}, []any{"a", nil}), "views: []\n", "views: []\nextra:\n  - a\n  - null\n")
	run(t, set([]any{"filters"}, `status != "done"`), "", "filters: status != \"done\"\n")
	run(t, set([]any{"filters", "and"}, []any{"x"}), "filters:\n", "filters:\n  and:\n    - x\n")
}

func TestStyles(t *testing.T) {
	// B-EDIT-01: CRLF, 4-space indentation, no final newline.
	src := "views:\r\n    -   type: table\r\n        name: A\r\nformulas:\r\n    a: \"1\""
	run(t, set([]any{"formulas", "b"}, "2"), src, src+"\r\n    b: \"2\"")
	run(t, set([]any{"views", 0, "name"}, "B"), src, strings.Replace(src, "name: A", "name: B", 1))
	// An indentless list under a key.
	src = "views:\n- type: table\n  name: A\n- type: cards\n  name: B\nother: 1\n"
	run(t, func(b []byte) ([]byte, error) { return Remove(b, []any{"views", 0}) }, src, "views:\n- type: cards\n  name: B\nother: 1\n")
	run(t, func(b []byte) ([]byte, error) { return Insert(b, []any{"views"}, 1, "x", Options{}) }, src,
		"views:\n- type: table\n  name: A\n- x\n- type: cards\n  name: B\nother: 1\n")
}

func TestRemoveAndInsert(t *testing.T) {
	// B-EDIT-01
	rm := func(path ...any) func([]byte) ([]byte, error) {
		return func(b []byte) ([]byte, error) { return Remove(b, path) }
	}
	run(t, rm("summaries"), example, strings.Replace(example, "summaries:\n  customAverage: 'values.mean().round(3)'\n", "", 1))
	run(t, rm("views", 1), example, strings.TrimSuffix(example, "  - type: kanban\n    name: Board\n"))
	run(t, rm("formulas", "formatted_price"), example, strings.Replace(example, "  formatted_price: 'if(price, price.toFixed(2) + \" dollars\")'\n", "", 1))
	// Removing the only key of a mapping rewrites its parent entry.
	run(t, rm("properties", "status"), example, strings.Replace(example, "properties:\n  status:\n    displayName: Status\n", "properties: {}\n", 1))
	// The first key of a list item shares the dash's line.
	run(t, rm("views", 1, "type"), example, strings.Replace(example, "  - type: kanban\n    name: Board\n", "  - name: Board\n", 1))
	ins := func(i int, v any, path ...any) func([]byte) ([]byte, error) {
		return func(b []byte) ([]byte, error) { return Insert(b, path, i, v, Options{}) }
	}
	run(t, ins(0, "file.hasTag(\"x\")", "filters", "or"), example,
		strings.Replace(example, "  or:\n", "  or:\n    - file.hasTag(\"x\")\n", 1))
	run(t, ins(-1, "z", "filters", "or", 1, "and"), example,
		strings.Replace(example, "        - file.hasLink(\"Textbook\")\n", "        - file.hasLink(\"Textbook\")\n        - z\n", 1))
}

func TestRefused(t *testing.T) {
	cases := []struct {
		f    func() ([]byte, error)
		want string
	}{
		{func() ([]byte, error) { return Set([]byte("a: [1"), []any{"a"}, 1, Options{}) }, "not valid YAML"},
		{func() ([]byte, error) { return Set([]byte(example), []any{"views", 5}, 1, Options{}) }, "views[5]: the index is out of range"},
		{func() ([]byte, error) { return Set([]byte(example), []any{"views", "x"}, 1, Options{}) }, "a key needs a mapping"},
		{func() ([]byte, error) { return Set([]byte(example), []any{"formulas", "ppu", "x"}, 1, Options{}) }, "not a mapping"},
		{func() ([]byte, error) { return Set([]byte(example), nil, 1, Options{}) }, "a path is required"},
		{func() ([]byte, error) { return Set([]byte(example), []any{"a"}, "\xff", Options{}) }, "UTF-8"},
		{func() ([]byte, error) { return Set([]byte(example), []any{"a"}, struct{}{}, Options{}) }, "can't be a"},
		{func() ([]byte, error) { return Remove([]byte(example), []any{"nope"}) }, "nothing here"},
		{func() ([]byte, error) { return Insert([]byte(example), []any{"formulas"}, 0, 1, Options{}) }, "not a list"},
		{func() ([]byte, error) { return Insert([]byte(example), []any{"views"}, 9, 1, Options{}) }, "out of range"},
	}
	for _, tc := range cases {
		_, err := tc.f()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("err = %v; want %q", err, tc.want)
		}
	}
}

func FuzzSet(f *testing.F) {
	f.Add(example, "formulas", "x", "v")
	f.Add("a: 1\n", "a", "", "2")
	f.Add("- a\n- b\n", "a", "", "")
	f.Add("a: {b: 1}\n", "a", "b", "\n")
	f.Fuzz(func(t *testing.T, src, k1, k2, v string) {
		path := []any{k1}
		if k2 != "" {
			path = append(path, k2)
		}
		out, err := Set([]byte(src), path, v, Options{})
		if err != nil {
			return
		}
		var got map[string]any
		if err := yaml.Unmarshal(out, &got); err != nil {
			t.Fatalf("result doesn't parse: %v\n%s", err, out)
		}
		cur := any(got)
		for _, s := range path {
			m, ok := cur.(map[string]any)
			if !ok {
				t.Fatalf("path not a mapping in\n%s", out)
			}
			cur = m[s.(string)]
		}
		if cur != v {
			t.Fatalf("value = %#v; want %q\n%s", cur, v, out)
		}
	})
}
