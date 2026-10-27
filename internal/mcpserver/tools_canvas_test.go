package mcpserver

import (
	"maps"
	"strings"
	"testing"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
)

func TestCanvasTools(t *testing.T) {
	// X-TOOL-11, C-EDIT-01, C-PRES-01, C-NODE-02
	h := start(t, false)
	before := testutil.TreeSums(t, h.dir)
	text, out := h.ok("create_canvas", map[string]any{"path": "Map.canvas", "dry_run": true, "ops": []any{
		map[string]any{"op": "add_node", "id": "a", "type": "text", "text": "Start", "x": 0, "y": 0, "width": 200, "height": 60},
	}})
	if out["dry_run"] != true || !strings.Contains(text, `"text":"Start"`) || !maps.Equal(before, testutil.TreeSums(t, h.dir)) {
		t.Errorf("X-SRV-05 create_canvas dry run = %s", text)
	}
	_, out = h.ok("create_canvas", map[string]any{"path": "Map.canvas", "ops": []any{
		map[string]any{"op": "add_node", "id": "a", "type": "text", "text": "Start", "x": 0, "y": 0, "width": 200, "height": 60},
		map[string]any{"op": "add_node", "type": "file", "file": "Welcome.md", "x": 300, "y": 0, "width": 400, "height": 300},
	}})
	added := out["added"].([]any)
	if len(added) != 2 || len(added[1].(string)) != 16 {
		t.Fatalf("C-NODE-02 added = %v", out)
	}
	etag := out["etag"]
	text, _ = h.ok("edit_canvas", map[string]any{"path": "Map.canvas", "etag": etag, "ops": []any{
		map[string]any{"op": "add_edge", "id": "e", "fromNode": "a", "toNode": added[1], "label": "next"},
		map[string]any{"op": "update_node", "id": "a", "color": "1", "x": 10},
	}})
	got := h.read("Map.canvas")
	want := "{\n\t\"nodes\":[\n\t\t{\"id\":\"a\",\"type\":\"text\",\"x\":10,\"y\":0,\"width\":200,\"height\":60,\"text\":\"Start\",\"color\":\"1\"},\n" +
		"\t\t{\"id\":\"" + added[1].(string) + "\",\"type\":\"file\",\"x\":300,\"y\":0,\"width\":400,\"height\":300,\"file\":\"Welcome.md\"}\n" +
		"\t],\n\t\"edges\":[\n\t\t{\"id\":\"e\",\"fromNode\":\"a\",\"toNode\":\"" + added[1].(string) + "\",\"label\":\"next\"}\n\t]\n}"
	if got != want {
		t.Errorf("C-PRES-01 edit_canvas wrote:\n%s\nwant:\n%s\n%s", got, want, text)
	}
	if _, out := h.ok("backlinks", map[string]any{"path": "Welcome.md"}); !strings.Contains(strings.Join(anyStrings(out["links"]), " "), "Map.canvas") {
		t.Errorf("canvas file node is a backlink: %v", out)
	}
	text, out = h.ok("read_canvas", map[string]any{"path": "Map.canvas", "limit": 2})
	if out["total_nodes"].(float64) != 2 || out["total_edges"].(float64) != 1 || len(out["nodes"].([]any)) != 2 || out["next_cursor"] != "2" ||
		!strings.Contains(text, `node a text (10,0 200x60) "Start" color 1`) || len(blocks(t, text)) != 2 {
		t.Errorf("X-TOOL-11 read_canvas = %s / %v", text, out)
	}
	_, out = h.ok("read_canvas", map[string]any{"path": "Map.canvas", "cursor": "2", "include_json": false})
	if es := out["edges"].([]any); len(es) != 1 || es[0].(map[string]any)["label"] != "next" {
		t.Errorf("read_canvas page 2 = %v", out)
	}
	etag = out["etag"]
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"path": "Map.canvas", "etag": "stale", "ops": []any{map[string]any{"op": "remove_node", "id": "a"}}}, "read_note again"},
		{map[string]any{"path": "Map.canvas", "etag": etag, "ops": []any{map[string]any{"op": "update_node", "id": "a", "color": "red"}}}, "read_canvas shows"},
		{map[string]any{"path": "Map.canvas", "etag": etag, "ops": []any{map[string]any{"op": "add_node", "type": "file", "file": "../x.md", "x": 0, "y": 0, "width": 1, "height": 1}}}, "dot segment"},
		{map[string]any{"path": "Welcome.md", "etag": etag, "ops": []any{map[string]any{"op": "remove_node", "id": "a"}}}, ".canvas files only"},
	} {
		if text := h.fail("edit_canvas", tc.args); !strings.Contains(text, tc.want) {
			t.Errorf("edit_canvas %v = %s; want %q", tc.args, text, tc.want)
		}
	}
	_, out = h.ok("edit_canvas", map[string]any{"path": "Map.canvas", "etag": etag, "ops": []any{map[string]any{"op": "remove_node", "id": "a"}}})
	if rm := out["removed_edges"].([]any); len(rm) != 1 || strings.Contains(h.read("Map.canvas"), `"e"`) {
		t.Errorf("C-EDIT-01 remove cascades: %v", out)
	}
	h.ok("undo_last_write", nil)
	if h.read("Map.canvas") != want {
		t.Error("undo didn't restore the canvas")
	}
	h.fail("create_canvas", map[string]any{"path": "Map.canvas"})
}

func TestBaseTools(t *testing.T) {
	// X-TOOL-11, B-EDIT-01, B-EMB-01
	h := start(t, false)
	text, out := h.ok("read_base", map[string]any{"path": "Tasks.base"})
	if vs := out["views"].([]any); len(vs) != 1 || !strings.Contains(text, `view "All" (table)`) {
		t.Errorf("X-TOOL-11 read_base = %s", text)
	}
	_, out = h.ok("edit_base", map[string]any{"path": "Tasks.base", "etag": out["etag"], "ops": []any{
		map[string]any{"op": "set", "path": []any{"formulas", "late"}, "value": "due < today()"},
		map[string]any{"op": "set", "path": []any{"views", "All", "limit"}, "value": 20},
		map[string]any{"op": "insert", "path": []any{"views"}, "value": map[string]any{"type": "kanban", "name": "Board", "groupBy": map[string]any{"property": "status", "direction": "ASC"}}},
		map[string]any{"op": "set", "path": []any{"properties", "formula.late", "displayName"}, "value": "Late"},
	}})
	want := "views:\n  - type: table\n    name: All\n    limit: 20\n  - type: kanban\n    name: Board\n    groupBy:\n      property: status\n      direction: ASC\n" +
		"formulas:\n  late: due < today()\nproperties:\n  formula.late:\n    displayName: Late\n"
	if got := h.read("Tasks.base"); got != want {
		t.Errorf("B-EDIT-01 edit_base wrote:\n%s\nwant:\n%s", got, want)
	}
	etag := out["etag"]
	if text := h.fail("edit_base", map[string]any{"path": "Tasks.base", "etag": etag, "ops": []any{
		map[string]any{"op": "set", "path": []any{"views", "Board", "groupBy", "direction"}, "value": "UP"},
	}}); !strings.Contains(text, "must be ASC or DESC") || !strings.Contains(text, "Hint:") {
		t.Errorf("edit_base invalid = %s", text)
	}
	if text := h.fail("edit_base", map[string]any{"path": "Tasks.base", "etag": etag, "ops": []any{
		map[string]any{"op": "remove", "path": []any{"views", "Nope"}},
	}}); !strings.Contains(text, `no view is named "Nope"`) {
		t.Errorf("edit_base unknown view = %s", text)
	}
	text, out = h.ok("create_base", map[string]any{"path": "Books.base", "content": "filters: file.hasTag(\"book\")\nviews:\n  - type: table\n    name: Books\n    order: [formula.x]\n"})
	if ps := out["problems"].([]any); len(ps) != 1 || !strings.Contains(text, "warning: views[0].order[0]") {
		t.Errorf("create_base warnings = %s", text)
	}
	if text := h.fail("create_base", map[string]any{"path": "Bad.base", "content": "views: 3\n"}); !strings.Contains(text, "views: must be a list") {
		t.Errorf("create_base invalid = %s", text)
	}
	h.write("Embeds.md", "![[Tasks.base#Board]]\n\n```base\nviews:\n  - type: table\n```\n")
	text, out = h.ok("read_base", map[string]any{"path": "Embeds.md"})
	if bs := out["blocks"].([]any); len(bs) != 1 || !strings.Contains(text, `problem: views[0]: "name" is required`) {
		t.Errorf("B-EMB-01 code block = %s", text)
	}
	if _, out := h.ok("outgoing_links", map[string]any{"path": "Embeds.md"}); !strings.Contains(strings.Join(anyStrings(out["links"]), " "), "Tasks.base") {
		t.Errorf("B-EMB-01 base embed resolves: %v", out)
	}
}

// anyStrings flattens a structured-output list of objects into their
// string values.
func anyStrings(v any) []string {
	var out []string
	for _, it := range v.([]any) {
		for _, f := range it.(map[string]any) {
			if s, ok := f.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
