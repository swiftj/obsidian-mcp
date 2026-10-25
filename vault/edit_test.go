package vault

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
	"github.com/swiftj/obsidian-mcp/vault/props"
)

const editDoc = `---
title: x
---
# Top

Intro text.

## Alpha

Alpha body.
	tabbed line

## Beta
Beta body ^beta-id

- item
- item 2

^list-id

### Beta Child

child

## Alpha

Second alpha.
`

func crlf(s string) string { return "\xef\xbb\xbf" + strings.ReplaceAll(s, "\n", "\r\n") }

func TestApplyEdits(t *testing.T) {
	// V-EDIT-01, V-EDIT-03
	tests := []struct {
		name string
		op   EditOp
		want string // the LF result
	}{
		{"replace section", EditOp{Kind: EditReplaceSection, Heading: "Top#Beta", Text: "New beta."},
			strings.Replace(editDoc, "Beta body ^beta-id\n\n- item\n- item 2\n\n^list-id\n\n### Beta Child\n\nchild\n", "New beta.\n", 1)},
		{"replace section multi-line text", EditOp{Kind: EditReplaceSection, Heading: "Beta#Beta Child", Text: "one\ntwo"},
			strings.Replace(editDoc, "### Beta Child\n\nchild\n", "### Beta Child\n\none\ntwo\n", 1)},
		{"insert after heading", EditOp{Kind: EditInsertAfterHeading, Heading: "Beta#Beta Child", Text: "Inserted."},
			strings.Replace(editDoc, "### Beta Child\n\nchild\n", "### Beta Child\n\nInserted.\nchild\n", 1)},
		{"case-insensitive heading", EditOp{Kind: EditInsertAfterHeading, Heading: "beta", Text: "First."},
			strings.Replace(editDoc, "## Beta\nBeta body", "## Beta\nFirst.\nBeta body", 1)},
		{"append", EditOp{Kind: EditAppend, Text: "Tail."}, editDoc + "Tail.\n"},
		{"prepend after frontmatter", EditOp{Kind: EditPrepend, Text: "Head.\nSecond."},
			strings.Replace(editDoc, "---\n# Top", "---\nHead.\nSecond.\n# Top", 1)},
		{"replace text", EditOp{Kind: EditReplaceText, Find: "body", Count: 2, Text: "BODY"},
			strings.NewReplacer("Alpha body.", "Alpha BODY.", "Beta body", "Beta BODY").Replace(editDoc)},
		{"replace paragraph block", EditOp{Kind: EditReplaceBlock, Block: "^beta-id", Text: "Replaced para.\n"},
			strings.Replace(editDoc, "Beta body ^beta-id", "Replaced para. ^beta-id", 1)},
		{"replace list block", EditOp{Kind: EditReplaceBlock, Block: "LIST-ID", Text: "- one"},
			strings.Replace(editDoc, "- item\n- item 2\n", "- one\n", 1)},
	}
	for _, tt := range tests {
		for _, variant := range []struct {
			name      string
			src, want string
		}{{"LF", editDoc, tt.want}, {"CRLF+BOM", crlf(editDoc), crlf(tt.want)}} {
			src := []byte(variant.src)
			out, span, err := applyEdit(src, tt.op)
			if err != nil {
				t.Errorf("V-EDIT-01 %s (%s): %v", tt.name, variant.name, err)
				continue
			}
			if string(out) != variant.want {
				t.Errorf("V-EDIT-03 %s (%s):\ngot:\n%q\nwant:\n%q", tt.name, variant.name, out, variant.want)
			}
			tail := len(src) - span.End
			if !bytes.Equal(out[:span.Start], src[:span.Start]) || !bytes.Equal(out[len(out)-tail:], src[span.End:]) {
				t.Errorf("V-EDIT-01 %s (%s): bytes outside %v changed", tt.name, variant.name, span)
			}
		}
	}
	// Ops apply in order.
	out, err := ApplyEdits([]byte(editDoc), []EditOp{{Kind: EditAppend, Text: "A"}, {Kind: EditAppend, Text: "B"}})
	if err != nil || string(out) != editDoc+"A\nB\n" {
		t.Errorf("V-EDIT-01 sequence = %q, %v", out, err)
	}
	// Append to a note without a final newline.
	if out, _ := ApplyEdits([]byte("no newline"), []EditOp{{Kind: EditAppend, Text: "x"}}); string(out) != "no newline\nx\n" {
		t.Errorf("V-EDIT-01 append without final newline = %q", out)
	}
}

func TestEditErrors(t *testing.T) {
	// V-EDIT-02
	tests := []struct {
		op         EditOp
		candidates int
		count      int
	}{
		{EditOp{Kind: EditReplaceSection, Heading: "Alpha", Text: "x"}, 2, 0},
		{EditOp{Kind: EditReplaceSection, Heading: "Top#Alpha", Text: "x"}, 2, 0},
		{EditOp{Kind: EditReplaceSection, Heading: "Missing", Text: "x"}, 0, 0},
		{EditOp{Kind: EditReplaceSection, Heading: "Beta#Alpha", Text: "x"}, 0, 0},
		{EditOp{Kind: EditReplaceText, Find: "body", Count: 3, Text: "x"}, 0, 2},
		{EditOp{Kind: EditReplaceText, Find: "absent", Count: 0, Text: "x"}, 0, 0},
		{EditOp{Kind: EditReplaceText, Find: "title", Count: 1, Text: "x"}, 0, 0}, // frontmatter is not body
		{EditOp{Kind: EditReplaceBlock, Block: "nope", Text: "x"}, 0, 0},
		{EditOp{Kind: "bogus"}, 0, 0},
	}
	for _, tt := range tests {
		_, err := ApplyEdits([]byte(editDoc), []EditOp{tt.op})
		var ee *EditError
		if !errors.As(err, &ee) {
			t.Errorf("V-EDIT-02 %+v: err = %v", tt.op, err)
			continue
		}
		if len(ee.Candidates) != tt.candidates || ee.Count != tt.count {
			t.Errorf("V-EDIT-02 %+v: candidates %v count %d", tt.op, ee.Candidates, ee.Count)
		}
	}
	dup := "a ^x\n\nb ^x\n"
	if _, err := ApplyEdits([]byte(dup), []EditOp{{Kind: EditReplaceBlock, Block: "x", Text: "c"}}); err == nil {
		t.Error("V-EDIT-02 duplicate block ID accepted")
	}
}

func TestVaultEdit(t *testing.T) {
	ctx := context.Background()
	v, dir := openBasic(t, Options{})
	f := mustRead(t, v, "Windows.md")
	// A dry run changes nothing.
	before := testutil.TreeSums(t, dir)
	res, err := v.Edit(ctx, "Windows.md", f.ETag, []EditOp{{Kind: EditAppend, Text: "More."}}, WriteOptions{DryRun: true})
	if err != nil || !strings.Contains(res.Diff, "+More.") {
		t.Fatalf("V-EDIT-01 dry run = %+v, %v", res, err)
	}
	if !maps.Equal(before, testutil.TreeSums(t, dir)) {
		t.Error("V-WRITE-07 edit dry run changed the disk")
	}
	if _, err := v.Edit(ctx, "Windows.md", f.ETag, []EditOp{{Kind: EditAppend, Text: "More.\nLines."}}, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, v, "Windows.md").Data); got != string(f.Data)+"More.\r\nLines.\r\n" {
		t.Errorf("V-EDIT-03 edited CRLF note = %q", got)
	}
	if _, err := v.Edit(ctx, "Windows.md", f.ETag, []EditOp{{Kind: EditAppend, Text: "x"}}, WriteOptions{}); err == nil {
		t.Error("V-WRITE-01 edit with a stale etag succeeded")
	}
	_, err = v.Edit(ctx, "Board.canvas", "x", nil, WriteOptions{})
	asPathError(t, err, "edit a canvas")
	// A mixed-ending file keeps its other line endings (no normalisation).
	mixed := "a\r\nb\nc\r\n"
	if err := os.WriteFile(filepath.Join(dir, "Mixed.md"), []byte(mixed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Edit(ctx, "Mixed.md", ETag([]byte(mixed)), []EditOp{{Kind: EditAppend, Text: "d"}}, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, v, "Mixed.md").Data); got != mixed+"d\r\n" {
		t.Errorf("V-EDIT-01 mixed endings = %q", got)
	}
}

func TestVaultProperties(t *testing.T) {
	// P-TYPE-01, P-TYPE-07 through the vault.
	ctx := context.Background()
	v, dir := openBasic(t, Options{})
	types := `{"types":{"count":"number","due":"date"}}`
	if err := os.WriteFile(filepath.Join(dir, ".obsidian", "types.json"), []byte(types), 0o644); err != nil {
		t.Fatal(err)
	}
	ty, err := v.PropertyTypes(ctx)
	if err != nil || ty["count"] != props.Number || ty["due"] != props.Date {
		t.Fatalf("P-TYPE-01 PropertyTypes = %v, %v", ty, err)
	}
	f := mustRead(t, v, "Welcome.md")
	_, err = v.SetProperties(ctx, "Welcome.md", f.ETag, map[string]any{"count": "lots"}, WriteOptions{})
	var pe *props.Error
	if !errors.As(err, &pe) || pe.Hint == "" {
		t.Errorf("P-TYPE-07 conflicting value: %v", err)
	}
	res, err := v.SetProperties(ctx, "Welcome.md", f.ETag, map[string]any{"count": 3, "status": "draft", "due": "2024-05-01"}, WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ncount: 3\ndue: 2024-05-01\nstatus: draft\n---\n" + string(f.Data)
	if got := string(mustRead(t, v, "Welcome.md").Data); got != want {
		t.Errorf("SetProperties = %q; want %q", got, want)
	}
	res, err = v.RenameProperty(ctx, "Welcome.md", res.ETag, "status", "state", WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.RemoveProperties(ctx, "Welcome.md", res.ETag, []string{"due", "absent"}, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, v, "Welcome.md").Data); got != "---\ncount: 3\nstate: draft\n---\n"+string(f.Data) {
		t.Errorf("rename+remove = %q", got)
	}
	if _, err := v.SetProperties(ctx, "Board.canvas", "x", map[string]any{"a": 1}, WriteOptions{}); err == nil {
		t.Error("properties on a canvas accepted")
	}
	// No types.json at all.
	if err := os.Remove(filepath.Join(dir, ".obsidian", "types.json")); err != nil {
		t.Fatal(err)
	}
	if ty, err := v.PropertyTypes(ctx); err != nil || len(ty) != 0 {
		t.Errorf("P-TYPE-01 missing types.json = %v, %v", ty, err)
	}
}
