package bases

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/swiftj/obsidian-mcp/vault/ofm"
)

// docsExample is the example from bases-syntax.md §Example.
const docsExample = `filters:
  or:
    - file.hasTag("tag")
    - and:
        - file.hasTag("book")
        - file.hasLink("Textbook")
    - not:
        - file.hasTag("book")
        - file.inFolder("Required Reading")
formulas:
  formatted_price: 'if(price, price.toFixed(2) + " dollars")'
  ppu: "(price / age).toFixed(2)"
properties:
  status:
    displayName: Status
  formula.formatted_price:
    displayName: "Price"
  file.ext:
    displayName: Extension
summaries:
  customAverage: 'values.mean().round(3)'
views:
  - type: table
    name: "My table"
    limit: 10
    groupBy:
      property: note.age
      direction: DESC
    filters:
      and:
        - 'status != "done"'
        - or:
            - "formula.ppu > 5"
            - "price > 2.1"
    order:
      - file.name
      - file.ext
      - note.age
      - formula.ppu
      - formula.formatted_price
    summaries:
      formula.ppu: Average
  - type: kanban
    name: Projects
    groupBy:
      property: note.status
      direction: ASC
    groupOrder:
      - Planned
      - In progress
      - Done
      - null
    plugin-setting: kept
`

func problems(t *testing.T, src string) []string {
	t.Helper()
	b, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var out []string
	for _, p := range b.Validate() {
		out = append(out, p.String())
	}
	return out
}

func TestDocsExample(t *testing.T) {
	// B-SCH-01, B-FLT-01, B-FRM-01, B-PROP-01, B-SUM-01, B-VIEW-01
	if ps := problems(t, docsExample); len(ps) != 0 {
		t.Fatalf("docs example has problems: %v", ps)
	}
	b, _ := Parse([]byte(docsExample))
	vs := b.Views()
	if len(vs) != 2 || vs[0].Name != "My table" || vs[0].Limit != 10 || vs[0].GroupBy != "note.age" || vs[0].Direction != "DESC" || !vs[0].Filtered || len(vs[0].Order) != 5 || vs[1].Type != "kanban" {
		t.Errorf("Views = %+v", vs)
	}
	if fs := b.Formulas(); len(fs) != 2 || fs[1] != (Named{"ppu", "(price / age).toFixed(2)"}) {
		t.Errorf("Formulas = %v", fs)
	}
	if ps := b.Properties(); !slices.Equal(ps, []string{"status", "formula.formatted_price", "file.ext"}) {
		t.Errorf("Properties = %v", ps)
	}
	if s := b.Summaries(); len(s) != 1 || !b.HasFilters() {
		t.Errorf("Summaries = %v", s)
	}
	for _, src := range []string{"", "# nothing\n", "filters: 'file.ext == \"md\"'\n", "unknown: 1\nviews: []\n"} {
		if ps := problems(t, src); len(ps) != 0 {
			t.Errorf("B-SCH-01 %q: %v", src, ps)
		}
	}
	if _, err := Parse([]byte("views: [")); err == nil {
		t.Error("broken YAML parsed")
	}
}

func TestValidateProblems(t *testing.T) {
	// B-SCH-01, B-FLT-01, B-FRM-01, B-PROP-01, B-SUM-01, B-VIEW-01
	view := func(extra string) string { return "views:\n  - type: table\n    name: A\n" + extra }
	cases := []struct{ src, want string }{
		{"- a\n", "a base must be a mapping, not a list"},
		{"filters: 3\n", "filters: a filter must be a statement string"},
		{"filters:\n  and: x\n", "filters.and: must be a list of filters"},
		{"filters:\n  xor: [a]\n", `filters: a filter mapping must have exactly one of and, or, not, not "xor"`},
		{"filters:\n  and: [a]\n  or: [b]\n", "filters: a filter mapping must have exactly one of and, or, not"},
		{"filters:\n  or:\n    - a\n    - not:\n        - [x]\n", "filters.or[1].not[0]: a filter must be"},
		{"formulas: [a]\n", "formulas: must be a mapping of names to formulas"},
		{"formulas:\n  a: 3\n", "formulas.a: must be a formula string, not a int"},
		{"properties:\n  status: Status\n", "properties.status: must be a mapping of settings"},
		{"properties:\n  formula.nope:\n    displayName: X\n", `warning: properties.formula.nope: no formula is named "nope"`},
		{"summaries:\n  x: [1]\n", "summaries.x: must be a formula string"},
		{"views: {}\n", "views: must be a list of views"},
		{"views:\n  - type: table\n", `views[0]: "name" is required`},
		{"views:\n  - name: A\n", `views[0]: "type" is required`},
		{"views:\n  - type: table\n    name: A\n  - type: cards\n    name: A\n", `views[1].name: another view is already named "A"`},
		{view("    limit: ten\n"), "views[0].limit: must be an integer"},
		{view("    limit: 1.5\n"), "views[0].limit: must be an integer"},
		{view("    order: file.name\n"), "views[0].order: must be a list of property IDs"},
		{view("    groupBy:\n      direction: ASC\n"), `views[0].groupBy: "property" is required`},
		{view("    groupBy:\n      property: a\n      direction: up\n"), "views[0].groupBy.direction: must be ASC or DESC"},
		{view("    groupOrder:\n      - [a]\n"), "views[0].groupOrder[0]: must be a value or null"},
		{view("    filters:\n      not: a\n"), "views[0].filters.not: must be a list of filters"},
		{view("    summaries:\n      price: Mean\n"), `views[0].summaries.price: "Mean" is neither a default summary`},
		{view("    summaries: [a]\n"), "views[0].summaries: must be a mapping"},
	}
	for _, tc := range cases {
		got := problems(t, tc.src)
		if !slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, tc.want) }) {
			t.Errorf("%q:\n got %q\nwant %q", tc.src, got, tc.want)
		}
	}
	if ps := problems(t, "summaries:\n  mine: values.sum()\n"+view("    summaries:\n      price: mine\n")); len(ps) != 0 {
		t.Errorf("B-SUM-01 custom summary: %v", ps)
	}
}

func TestApply(t *testing.T) {
	// B-EDIT-01
	src := "# Tasks\nviews:\n  - type: table\n    name: All # main\n"
	r, err := Apply([]byte(src), []Op{
		{Kind: Set, Path: []any{"formulas", "late"}, Value: `due < today()`},
		{Kind: Insert, Path: []any{"views"}, Index: -1, Value: map[string]any{"name": "Board", "type": "kanban", "groupBy": map[string]any{"direction": "ASC", "property": "note.status"}}},
		{Kind: Set, Path: []any{"views", "All", "limit"}, Value: float64(5)},
		{Kind: Set, Path: []any{"views", float64(1), "order"}, Value: []any{"file.name"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Tasks\nviews:\n  - type: table\n    name: All # main\n    limit: 5\n" +
		"  - type: kanban\n    name: Board\n    groupBy:\n      property: note.status\n      direction: ASC\n    order:\n      - file.name\n" +
		"formulas:\n  late: due < today()\n"
	if string(r.Data) != want {
		t.Errorf("B-EDIT-01 got:\n%s\nwant:\n%s", r.Data, want)
	}
	r, err = Apply([]byte(want), []Op{{Kind: Remove, Path: []any{"views", "Board"}}})
	if err != nil || string(r.Data) != "# Tasks\nviews:\n  - type: table\n    name: All # main\n    limit: 5\nformulas:\n  late: due < today()\n" {
		t.Errorf("remove view: %v\n%s", err, r.Data)
	}
	refused := []struct {
		ops  []Op
		want string
	}{
		{[]Op{{Kind: Set, Path: []any{"views", "Nope", "limit"}, Value: 1}}, `op 1: no view is named "Nope"`},
		{[]Op{{Kind: Set, Path: []any{"views", "All", "limit"}, Value: "x"}}, "views[0].limit: must be an integer"},
		{[]Op{{Kind: Remove, Path: []any{"views", 0, "name"}}}, `views[0]: "name" is required`},
		{[]Op{{Kind: "rename", Path: []any{"x"}}}, "unknown op"},
		{[]Op{{Kind: Set, Path: nil}}, "a path is required"},
		{[]Op{{Kind: Set, Path: []any{"views", 0.5}}}, "is not an index"},
	}
	for _, tc := range refused {
		_, err := Apply([]byte(src), tc.ops)
		var be *Error
		if !errors.As(err, &be) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("err = %v; want %q", err, tc.want)
		}
	}
	// Existing problems and warnings don't block; they're reported.
	r, err = Apply([]byte("views:\n  - type: table\nproperties:\n  formula.x: {}\n"), []Op{{Kind: Set, Path: []any{"properties", "formula.y"}, Value: map[string]any{}}})
	if err != nil || len(r.Existing) != 3 {
		t.Errorf("existing = %v, %v", err, r.Existing)
	}
}

func TestCodeBlocks(t *testing.T) {
	// B-EMB-01
	src := "# Note\n\n```base\nviews:\n  - type: table\n    name: A\n```\n\n> ```base\n> views: 1\n> ```\n\n~~~base\nviews: {}\n~~~\n\n```yaml\nviews: 1\n```\n"
	bs := CodeBlocks([]byte(src), ofm.Parse([]byte(src)))
	if len(bs) != 2 || bs[0].Line != 3 || bs[0].Text != "views:\n  - type: table\n    name: A\n" || bs[1].Line != 13 {
		t.Fatalf("CodeBlocks = %+v", bs)
	}
	b, _ := Parse([]byte(bs[1].Text))
	if ps := b.Validate(); len(ps) != 1 || ps[0].Path != "views" {
		t.Errorf("B-EMB-01 block validated like a file: %v", ps)
	}
}

func FuzzValidate(f *testing.F) {
	f.Add(docsExample)
	f.Add("views: [{type: a, name: b, limit: 1}]")
	f.Add("filters: {and: [{or: [a]}, {not: []}]}")
	f.Fuzz(func(t *testing.T, src string) {
		b, err := Parse([]byte(src))
		if err != nil {
			return
		}
		b.Validate()
		b.Views()
		b.Formulas()
		r, err := Apply([]byte(src), nil)
		if err != nil || string(r.Data) != src {
			t.Fatalf("no-op Apply changed the file: %v", err)
		}
	})
}
