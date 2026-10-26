package props

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
	"github.com/swiftj/obsidian-mcp/vault/ofm"
)

func read(src string) *Frontmatter { return Read(ofm.Parse([]byte(src))) }

func probeFile(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testutil.RepoRoot(), "testdata", "probes", "vault", rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestInferenceProbe(t *testing.T) {
	// P-TYPE-06: the D-006 inference table, from the probe vault's pr09/Infer.md.
	f := Read(ofm.Parse(probeFile(t, "pr09/Infer.md")))
	want := map[string]Type{
		"i_int": Number, "i_dec": Number, "i_bool": Checkbox, "i_date": Date, "i_dt": Text, "i_dt_nosec": Datetime,
		"i_text": Text, "i_quoted_num": Text, "i_quoted_date": Date, "i_link": Text, "i_list": Multitext,
		"i_flow": Multitext, "i_empty": Text, "i_null": Text, "i_nested": Unknown,
	}
	for _, p := range f.Props {
		if p.Inferred != want[p.Name] {
			t.Errorf("P-TYPE-06 %s inferred %s; want %s", p.Name, p.Inferred, want[p.Name])
		}
	}
	if len(f.Props) != len(want) {
		t.Errorf("P-TYPE-06 %d props", len(f.Props))
	}
	if p := f.Get("i_date"); p.Value != "2024-02-29" || p.Raw != "2024-02-29" {
		t.Errorf("P-DATE-01 date read as %#v raw %q", p.Value, p.Raw)
	}
	unk := read("---\nrel: [[Target14]]\n---\n").Get("rel")
	if unk.Inferred != Unknown {
		t.Errorf("P-TYPE-06 unquoted link inferred %s", unk.Inferred)
	}
}

func TestParseTypes(t *testing.T) {
	// P-TYPE-01: the D-006 types.json.
	ty, err := ParseTypes([]byte(`{"types":{"aliases":"aliases","cssclasses":"multitext","tags":"tags","p_num":"number","p_dt":"datetime"}}`))
	if err != nil || ty["p_num"] != Number || ty["p_dt"] != Datetime || ty["tags"] != Tags {
		t.Errorf("P-TYPE-01 types = %v, %v", ty, err)
	}
	if _, err := ParseTypes([]byte(`{`)); err == nil {
		t.Error("P-TYPE-01 bad types.json accepted")
	}
	f := read("---\np_num: 42\nother: x\n---\n")
	if TypeOf(f.Get("p_num"), ty) != Number || TypeOf(f.Get("other"), ty) != Text {
		t.Error("P-TYPE-01 TypeOf")
	}
}

func TestReadShapes(t *testing.T) {
	// P-KEY-01 duplicates, P-KEY-02 deprecated keys, O-FM-02 JSON.
	f := read("---\na: 1\nb: 2\na: 3\ntag: x\ntags: y\n---\n")
	if !slices.Equal(f.Duplicates, []string{"a", "tags"}) {
		t.Errorf("P-KEY-01 duplicates = %v", f.Duplicates)
	}
	if f.Get("tags").Key != "tag" {
		t.Error("P-KEY-02 tag not read as tags")
	}
	for k, want := range map[string]string{"tag": "tags", "alias": "aliases", "cssclass": "cssclasses", "x": "x"} {
		if Canonical(k) != want {
			t.Errorf("P-KEY-02 Canonical(%s) = %s", k, Canonical(k))
		}
	}
	j := Read(ofm.Parse(probeFile(t, "pr12/json.md")))
	if !j.JSON || j.Get("key").Value != "value" || !reflect.DeepEqual(j.Get("tags").Value, []any{"jsontag"}) {
		t.Errorf("O-FM-02 JSON props = %+v", j.Props)
	}
	if read("---\nkey: [unclosed\n---\n").Err == nil {
		t.Error("invalid YAML not reported")
	}
	if f := read("---\n---\nbody\n"); !f.Present || len(f.Props) != 0 || f.Err != nil {
		t.Errorf("empty frontmatter = %+v", f)
	}
}

func TestFrontmatterLinks(t *testing.T) {
	// O-LINK-06, from the probe vault's pr14 frontmatter cases.
	for file, want := range map[string]struct {
		path string
		line int
	}{
		"pr14/in-frontmatter-quoted.md":   {"rel", 2},
		"pr14/in-frontmatter-list.md":     {"rels.0", 3},
		"pr14/in-frontmatter-unquoted.md": {"", 0},
	} {
		f := Read(ofm.Parse(probeFile(t, file)))
		var got string
		var line int
		if len(f.Links) == 1 && f.Links[0].Target == "Target14" {
			got, line = f.Links[0].Path, f.Links[0].Line
		}
		if got != want.path || line != want.line || (want.path == "" && len(f.Links) != 0) {
			t.Errorf("O-LINK-06 %s links = %+v; want %+v", file, f.Links, want)
		}
	}
}

func TestFrontmatterTags(t *testing.T) {
	// O-TAG-02, from the probe vault's pr15 cases.
	tests := map[string][]string{
		"fm-scalar.md": {"solo"}, "fm-comma.md": nil, "fm-space.md": nil, "fm-hash.md": {"hashy"},
		"fm-flow.md": {"flow1", "flow2"}, "fm-singular.md": nil, "fm-nested.md": {"parent/child"},
	}
	for file, want := range tests {
		if got := Read(ofm.Parse(probeFile(t, "pr15/"+file))).Tags(); !slices.Equal(got, want) {
			t.Errorf("O-TAG-02 %s tags = %q; want %q", file, got, want)
		}
	}
}

func TestSetPreservesOtherBytes(t *testing.T) {
	// P-FM-01: only the changed key's lines differ.
	src := "---\n# leading comment\ntitle:   'Odd  quoting'   # trailing\ntags:\n- a\n- b\nmulti: |\n  line1\n  # not a comment\n\n# comment above date\ndate: 2024-01-15\nflow: [x, y]\nnested:\n  inner: 1\n---\nBody stays.\n"
	tests := []struct {
		name  string
		value any
		as    Type
		want  string // the new entry
		repl  string // the old entry it replaces ("" to append)
	}{
		{"title", "New", "", "title: New\n", "title:   'Odd  quoting'   # trailing\n"},
		{"tags", []string{"c", "#d"}, "", "tags:\n- c\n- d\n", "tags:\n- a\n- b\n"},
		{"multi", "one line", "", "multi: one line\n", "multi: |\n  line1\n  # not a comment\n"},
		{"date", "2024-02-29", "", "date: 2024-02-29\n", "date: 2024-01-15\n"},
		{"flow", []string{"p", "q,r"}, "", "flow: [p, \"q,r\"]\n", "flow: [x, y]\n"},
		{"added", 7, "", "added: 7\n", ""},
		{"link", "[[Note]]", "", "link: \"[[Note]]\"\n", ""},
		{"when", "2024-03-01T09:30", Datetime, "when: 2024-03-01T09:30:00\n", ""},
	}
	for _, tt := range tests {
		got, err := Set([]byte(src), tt.name, tt.value, SetOptions{As: tt.as})
		if err != nil {
			t.Errorf("P-FM-01 set %s: %v", tt.name, err)
			continue
		}
		var want string
		if tt.repl == "" {
			want = strings.Replace(src, "  inner: 1\n---\n", "  inner: 1\n"+tt.want+"---\n", 1)
		} else {
			want = strings.Replace(src, tt.repl, tt.want, 1)
		}
		if string(got) != want {
			t.Errorf("P-FM-01 set %s:\ngot:\n%s\nwant:\n%s", tt.name, got, want)
		}
	}
	got, err := Remove([]byte(src), "date")
	if err != nil || string(got) != strings.Replace(src, "date: 2024-01-15\n", "", 1) {
		t.Errorf("P-FM-01 remove = %q, %v", got, err)
	}
	got, err = Rename([]byte(src), "title", "heading")
	if err != nil || string(got) != strings.Replace(src, "title:   'Odd", "heading:   'Odd", 1) {
		t.Errorf("P-FM-01 rename = %q, %v", got, err)
	}
}

func TestSetStyles(t *testing.T) {
	// P-FM-01 across CRLF, BOM and missing frontmatter; P-KEY-02 plural writes.
	tests := []struct {
		src, name string
		value     any
		want      string
	}{
		{"---\r\na: 1\r\n---\r\nbody\r\n", "b", "x", "---\r\na: 1\r\nb: x\r\n---\r\nbody\r\n"},
		{"\xef\xbb\xbf---\na: 1\n---\n", "a", 2, "\xef\xbb\xbf---\na: 2\n---\n"},
		{"Just a body.\n", "k", "v", "---\nk: v\n---\nJust a body.\n"},
		{"\xef\xbb\xbfBody\r\n", "k", "v", "\xef\xbb\xbf---\r\nk: v\r\n---\r\nBody\r\n"},
		{"", "k", true, "---\nk: true\n---\n"},
		{"---\n---\nb\n", "k", "v", "---\nk: v\n---\nb\n"},
		{"---\ntag: old\n---\n", "tags", []string{"new"}, "---\ntags:\n  - new\n---\n"},
		{"---\ntag: old\n---\n", "tag", []string{"new"}, "---\ntags:\n  - new\n---\n"},
		{"---\nlist:\n    -   a\n---\n", "list", []any{"b", 2}, "---\nlist:\n    -   b\n    -   \"2\"\n---\n"},
	}
	for _, tt := range tests {
		got, err := Set([]byte(tt.src), tt.name, tt.value, SetOptions{})
		if err != nil || string(got) != tt.want {
			t.Errorf("P-FM-01 Set(%q, %s) = %q, %v; want %q", tt.src, tt.name, got, err, tt.want)
		}
	}
	got, _ := Rename([]byte("---\n\"quoted key\": 1\n---\n"), "quoted key", "plain")
	if string(got) != "---\nplain: 1\n---\n" {
		t.Errorf("rename quoted key = %q", got)
	}
}

func TestSetValidation(t *testing.T) {
	types := Types{"n": Number, "c": Checkbox, "d": Date, "dt": Datetime, "l": Multitext, "t": Text, "x": Tags}
	ok := []struct {
		name  string
		value any
		want  string
	}{
		{"n", 3.5, "n: 3.5"}, {"n", "-12", "n: -12"}, {"n", 42, "n: 42"}, // P-TYPE-02
		{"c", true, "c: true"}, {"c", nil, "c:"}, // P-TYPE-03
		{"d", "2024-02-29", "d: 2024-02-29"}, {"dt", "2024-02-29T23:59:59", "dt: 2024-02-29T23:59:59"}, // P-TYPE-04
		{"t", "42", "t: \"42\""}, {"t", "a: b", "t: 'a: b'"}, {"t", "[[x|y]]", "t: \"[[x|y]]\""}, // P-LINK-01
		{"l", []string{"[[A]]", "plain"}, "l:\n  - \"[[A]]\"\n  - plain"}, // P-LINK-01
	}
	for _, tt := range ok {
		got, err := Set([]byte("---\n---\n"), tt.name, tt.value, SetOptions{Types: types})
		if err != nil || string(got) != "---\n"+tt.want+"\n---\n" {
			t.Errorf("Set %s=%v = %q, %v; want %q", tt.name, tt.value, got, err, tt.want)
		}
	}
	bad := []struct {
		id, name string
		value    any
		as       Type
	}{
		{"P-TYPE-02", "n", "1+1", ""}, {"P-TYPE-02", "n", "ten", ""},
		{"P-TYPE-03", "c", "yes", ""},
		{"P-TYPE-04", "d", "2024-02-30", ""}, {"P-TYPE-04", "d", "15/01/2024", ""}, {"P-TYPE-04", "dt", "2024-01-15T25:00:00", ""},
		{"P-TYPE-05", "x", []string{"a"}, ""}, {"P-TYPE-05", "other", []string{"a"}, Tags},
		{"P-TYPE-07", "l", map[string]any{"a": 1}, ""}, {"P-TYPE-07", "n", []string{"1"}, ""},
		{"P-KEY-03", "new", map[string]any{"a": 1}, ""},
	}
	for _, tt := range bad {
		_, err := Set([]byte("---\n---\n"), tt.name, tt.value, SetOptions{Types: types, As: tt.as})
		var pe *Error
		if !errors.As(err, &pe) {
			t.Errorf("%s Set %s=%v accepted", tt.id, tt.name, tt.value)
		} else if tt.id == "P-TYPE-07" && pe.Hint == "" && tt.name != "l" {
			t.Errorf("P-TYPE-07 %s: no hint", tt.name)
		}
	}
	_, err := Set([]byte("---\n---\n"), "n", "x", SetOptions{Types: types})
	var pe *Error
	if !errors.As(err, &pe) || !strings.Contains(pe.Reason, "number property") || pe.Hint == "" {
		t.Errorf("P-TYPE-07 conflict message = %v", err)
	}
	if _, err := Set([]byte("---\n---\n"), "tags", []string{"two words"}, SetOptions{}); err == nil {
		t.Error("P-TYPE-05 invalid tag accepted")
	}
}

func TestRefusals(t *testing.T) {
	// P-JSON-01, P-KEY-01, P-KEY-03
	json := "---\n{\"a\": 1}\n---\n"
	if _, err := Set([]byte(json), "a", 2, SetOptions{}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("P-JSON-01 set on JSON: %v", err)
	}
	if _, err := Remove([]byte(json), "a"); err == nil {
		t.Error("P-JSON-01 remove on JSON")
	}
	dup := "---\na: 1\na: 2\n---\n"
	if _, err := Set([]byte(dup), "a", 3, SetOptions{}); err == nil {
		t.Error("P-KEY-01 set on a duplicate key")
	}
	if _, err := Rename([]byte("---\na: 1\nb: 2\n---\n"), "a", "b"); err == nil {
		t.Error("P-KEY-01 rename onto an existing key")
	}
	nested := "---\nn:\n  inner: 1\nx: 1\n---\n"
	if _, err := Set([]byte(nested), "n", "flat", SetOptions{}); err == nil {
		t.Error("P-KEY-03 overwrote a nested value")
	}
	got, err := Set([]byte(nested), "x", 2, SetOptions{})
	if err != nil || string(got) != "---\nn:\n  inner: 1\nx: 2\n---\n" {
		t.Errorf("P-KEY-03 nested value not preserved: %q, %v", got, err)
	}
	if got, err := Remove([]byte("body\n"), "x"); err != nil || string(got) != "body\n" {
		t.Error("remove without frontmatter")
	}
}

// FuzzPatch checks P-FM-02: after Set, reading the note back gives the new
// value, and every other property keeps its bytes and value.
func FuzzPatch(f *testing.F) {
	seeds := []string{
		"---\na: 1\nb: text\n---\nbody\n",
		"---\r\nlist:\r\n  - x\r\n  - y\r\nq: \"quoted\"\r\n---\r\n",
		"---\n# c\nk: v # c\n\n# c2\nm: |\n  block\n---\n",
		"no frontmatter\n",
		"---\nflow: [a, b]\nd: 2024-01-15\n---\n",
	}
	for _, s := range seeds {
		f.Add(s, "a", "value")
		f.Add(s, "new key", "with: colon")
	}
	f.Fuzz(func(t *testing.T, src, key, val string) {
		out, err := Set([]byte(src), key, val, SetOptions{As: Text})
		if err != nil {
			return
		}
		before := read(src)
		after := Read(ofm.Parse(out))
		if after.Err != nil {
			t.Fatalf("P-FM-02 patched YAML is invalid: %v\n%q", after.Err, out)
		}
		name := Canonical(strings.TrimSpace(key))
		got := after.Get(name)
		emptyOK := val == "" && got != nil && got.Value == nil
		if got == nil || (got.Value != val && !emptyOK) {
			t.Fatalf("P-FM-02 %q = %#v after set to %q\n%q", name, got, val, out)
		}
		for _, p := range before.Props {
			if p.Name == name || slices.Contains(before.Duplicates, p.Name) {
				continue
			}
			q := after.Get(p.Name)
			if q == nil || q.Raw != p.Raw || !reflect.DeepEqual(q.Value, p.Value) ||
				string(out[q.Lines.Start:q.Lines.End]) != src[p.Lines.Start:p.Lines.End] {
				t.Fatalf("P-FM-02 %q changed: %#v → %#v\n%q", p.Name, p, q, out)
			}
		}
	})
}

func TestRenameTag(t *testing.T) {
	// P-TAGREN-01
	src := "---\ntags:\n  - Proj\n  - proj/Sub\n  - other\n# keep\nx: 1\n---\n# Head #proj\n\nText #PROJ/deep/er and #project and `#proj` %% #proj %%\n\n```\n#proj\n```\n"
	out, n, err := RenameTag([]byte(src), "#proj", "work/main")
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ntags:\n  - work/main\n  - work/main/Sub\n  - other\n# keep\nx: 1\n---\n# Head #work/main\n\nText #work/main/deep/er and #project and `#proj` %% #proj %%\n\n```\n#proj\n```\n"
	if string(out) != want || n != 4 {
		t.Errorf("P-TAGREN-01 = %d\n%s\nwant\n%s", n, out, want)
	}
	// A one-word scalar becomes a list, the form Obsidian writes for tags.
	if out, n, err := RenameTag([]byte("---\ntags: proj\n---\n"), "proj", "w"); err != nil || n != 1 || string(out) != "---\ntags:\n  - w\n---\n" {
		t.Errorf("P-TAGREN-01 scalar = %q %d %v", out, n, err)
	}
	if out, n, _ := RenameTag([]byte("no tags\n"), "proj", "w"); n != 0 || string(out) != "no tags\n" {
		t.Errorf("P-TAGREN-01 no tags = %q", out)
	}
	for _, bad := range []string{"", "two words", "123", "a#b"} {
		if _, _, err := RenameTag([]byte("#proj\n"), "proj", bad); err == nil {
			t.Errorf("P-TAGREN-01 new tag %q accepted", bad)
		}
	}
}
