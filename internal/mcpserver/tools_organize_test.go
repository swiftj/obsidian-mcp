package mcpserver

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
)

func (h *harness) read(rel string) string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.dir, filepath.FromSlash(rel)))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

func TestMoveTools(t *testing.T) {
	// X-TOOL-08, L-REN-05
	h := start(t, false)
	before := testutil.TreeSums(t, h.dir)
	text, out := h.ok("move_note", map[string]any{"path": "Other.md", "to": "Archive/Other Renamed.md", "dry_run": true})
	if out["dry_run"] != true || !strings.Contains(text, "Welcome.md:3 [[Other]] -> [[Other Renamed]]") || !strings.Contains(text, "rename Other.md → Archive/Other Renamed.md") {
		t.Errorf("L-REN-05 move dry run = %s", text)
	}
	if !maps.Equal(before, testutil.TreeSums(t, h.dir)) {
		t.Error("X-SRV-05 move dry run changed the disk")
	}
	_, out = h.ok("move_note", map[string]any{"path": "Other.md", "to": "Archive/Other Renamed.md"})
	if out["links_rewritten"].(float64) != 1 || !strings.Contains(h.read("Welcome.md"), "[[Other Renamed]]") {
		t.Errorf("X-TOOL-08 move_note = %v", out)
	}
	h.ok("rename_note", map[string]any{"path": "Archive/Other Renamed.md", "new_name": "Other"})
	if !strings.Contains(h.read("Welcome.md"), "[[Other]]") {
		t.Errorf("X-TOOL-08 rename_note: %s", h.read("Welcome.md"))
	}
	h.fail("rename_note", map[string]any{"path": "Archive/Other.md", "new_name": "x/y"})
	h.ok("move_folder", map[string]any{"from": "Archive", "to": "Old/Archive"})
	if _, err := os.Stat(filepath.Join(h.dir, "Old", "Archive", "Other.md")); err != nil {
		t.Errorf("X-TOOL-08 move_folder: %v", err)
	}
	h.fail("move_folder", map[string]any{"from": "Old", "to": "Folder"})
	_, out = h.ok("create_folder", map[string]any{"path": "Fresh/Sub"})
	if out["changed"] != true {
		t.Errorf("X-TOOL-08 create_folder = %v", out)
	}
	// Undo walks back: create_folder isn't journaled; the folder move, then
	// the rename, then the move are undone in turn.
	for range 3 {
		h.ok("undo_last_write", nil)
	}
	if err := os.RemoveAll(filepath.Join(h.dir, "Fresh")); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(before, testutil.TreeSums(t, h.dir)) {
		t.Error("X-TOOL-10 three undos didn't restore the vault")
	}
	if text := h.fail("undo_last_write", nil); !strings.Contains(text, "nothing to undo") {
		t.Errorf("X-TOOL-10 empty undo = %s", text)
	}
}

func TestRenameTools(t *testing.T) {
	// X-TOOL-08
	h := start(t, false)
	h.write("H.md", "---\nstatus: draft\ntags: [proj]\n---\n# Old Head\npara ^blk\n\n[[#Old Head]] [[H#^blk]] #proj/x\n")
	_, out := h.ok("read_note", map[string]any{"path": "H.md"})
	h.ok("rename_heading", map[string]any{"path": "H.md", "etag": out["etag"], "heading": "Old Head", "new_heading": "New Head"})
	if !strings.Contains(h.read("H.md"), "# New Head\n") || !strings.Contains(h.read("H.md"), "[[#New Head]]") {
		t.Errorf("X-TOOL-08 rename_heading: %s", h.read("H.md"))
	}
	text := h.fail("rename_heading", map[string]any{"path": "H.md", "etag": "stale", "heading": "New Head", "new_heading": "X"})
	if !strings.Contains(text, "read_note again") {
		t.Errorf("X-SRV-10 rename_heading conflict = %s", text)
	}
	_, out = h.ok("read_note", map[string]any{"path": "H.md"})
	h.ok("rename_block", map[string]any{"path": "H.md", "etag": out["etag"], "block": "blk", "new_block": "blk2"})
	if !strings.Contains(h.read("H.md"), "[[H#^blk2]]") {
		t.Errorf("X-TOOL-08 rename_block: %s", h.read("H.md"))
	}
	_, out = h.ok("rename_tag", map[string]any{"old": "#proj", "new": "work"})
	if !strings.Contains(h.read("H.md"), "#work/x") || !strings.Contains(h.read("H.md"), "tags: [work]") || len(out["changed_files"].([]any)) != 1 {
		t.Errorf("X-TOOL-08 rename_tag: %v %s", out, h.read("H.md"))
	}
	h.write("Has.md", "---\nstatus: a\nstate: b\n---\n")
	text, out = h.ok("rename_property", map[string]any{"old": "status", "new": "state"})
	if !strings.Contains(h.read("H.md"), "state: draft") || len(out["skipped"].([]any)) != 1 || !strings.Contains(text, "skipped Has.md") {
		t.Errorf("X-TOOL-08 rename_property: %s / %v", text, out)
	}
}

func TestDailyAndTemplateTools(t *testing.T) {
	// X-TOOL-09
	h := start(t, false)
	h.write(".obsidian/daily-notes.json", `{"folder":"Daily","template":"Tpl/Day"}`)
	h.write(".obsidian/templates.json", `{"folder":"Tpl"}`)
	h.write("Tpl/Day.md", "# {{title}}\n")
	h.write("Tpl/Meet.md", "Meeting {{title}} on {{date}} <% ignore %>\n")
	text, out := h.ok("open_daily_note", map[string]any{"date": "2024-02-29", "dry_run": true})
	if out["created"] != true || out["path"] != "Daily/2024-02-29.md" || !strings.Contains(text, "# 2024-02-29") {
		t.Errorf("X-TOOL-09 open dry run = %s", text)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "Daily")); !os.IsNotExist(err) {
		t.Error("X-SRV-05 open_daily_note dry run created the note")
	}
	h.ok("append_to_daily_note", map[string]any{"date": "2024-02-29", "text": "- did a thing"})
	if got := h.read("Daily/2024-02-29.md"); got != "# 2024-02-29\n- did a thing\n" {
		t.Errorf("X-TOOL-09 append = %q", got)
	}
	_, out = h.ok("open_daily_note", map[string]any{"date": "2024-02-29"})
	if out["created"] != false {
		t.Errorf("X-TOOL-09 reopen = %v", out)
	}
	h.fail("open_daily_note", map[string]any{"date": "29/02/2024"})
	_, out = h.ok("list_templates", nil)
	if fmt.Sprint(out["paths"]) != "[Tpl/Day.md Tpl/Meet.md]" {
		t.Errorf("X-TOOL-09 list_templates = %v", out)
	}
	h.ok("create_from_template", map[string]any{"template": "Meet", "path": "Notes/Standup.md"})
	want := "Meeting Standup on " + time.Now().Format("2006-01-02") + " <% ignore %>\n"
	if got := h.read("Notes/Standup.md"); got != want {
		t.Errorf("X-TOOL-09 create_from_template = %q", got)
	}
}

func TestAttachTool(t *testing.T) {
	// X-TOOL-10, V-ATT-01
	h := start(t, false)
	src := filepath.Join(t.TempDir(), "photo one.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.write(".obsidian/app.json", `{"attachmentFolderPath":"./assets"}`)
	_, out := h.ok("attach_file", map[string]any{"file": src, "note": "Folder/Sub/Deep.md"})
	if out["path"] != "Folder/Sub/assets/photo one.png" || out["embed"] != "![[photo one.png]]" {
		t.Errorf("V-ATT-01 attach = %v", out)
	}
	_, out = h.ok("attach_file", map[string]any{"file": src, "note": "Folder/Sub/Deep.md"})
	if out["path"] != "Folder/Sub/assets/photo one 1.png" {
		t.Errorf("V-ATT-01 second attach = %v", out)
	}
	h.write(".obsidian/app.json", `{"attachmentFolderPath":"Media","useMarkdownLinks":true}`)
	_, out = h.ok("attach_file", map[string]any{"file": src, "name": "pic.png", "dry_run": true})
	if out["path"] != "Media/pic.png" || out["embed"] != "![](pic.png)" {
		t.Errorf("V-ATT-01 markdown attach = %v", out)
	}
	for _, f := range []string{"relative.png", filepath.Join(h.dir, "Welcome.md"), "/Users/x/.ssh/id.png", filepath.Join(t.TempDir(), "missing.png")} {
		h.fail("attach_file", map[string]any{"file": f})
	}
	link := filepath.Join(t.TempDir(), "link.png")
	if err := os.Symlink(src, link); err != nil {
		t.Fatal(err)
	}
	h.fail("attach_file", map[string]any{"file": link})
	for setting, want := range map[string]string{"": "", "/": "", "./": "A", "./x/": "A/x", "Files/": "Files"} {
		if got := attachmentFolder(setting, "A/n.md"); got != want {
			t.Errorf("attachmentFolder(%q) = %q; want %q", setting, got, want)
		}
	}
}
