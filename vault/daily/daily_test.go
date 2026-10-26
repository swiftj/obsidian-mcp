package daily

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/swiftj/obsidian-mcp/vault"
)

var when = time.Date(2024, time.January, 3, 15, 4, 5, 0, time.UTC) // a Wednesday

func TestFormat(t *testing.T) {
	// V-DAILY-01
	for layout, want := range map[string]string{
		"YYYY-MM-DD":            "2024-01-03",
		"YYYY/MMMM/YYYY-MMM-DD": "2024/January/2024-Jan-03",
		"dddd, Do MMMM YY":      "Wednesday, 3rd January 24",
		"ddd dd d E":            "Wed We 3 3",
		"[Week] W, GGGG-[W]WW":  "Week 1, 2024-W01",
		"Q DDD DDDD":            "1 3 003",
		"H:mm:ss h hh A a":      "15:04:05 3 03 PM pm",
		"YYYY-MM-DDTHH:mm":      "2024-01-03T15:04",
		"M/D":                   "1/3",
		"[YYYY] \\Y":            "YYYY Y",
	} {
		got, err := Format(when, layout)
		if err != nil || got != want {
			t.Errorf("V-DAILY-01 Format(%q) = %q, %v; want %q", layout, got, err, want)
		}
	}
	for _, d := range []int{1, 2, 3, 4, 11, 12, 13, 21, 22, 23, 31} {
		got, _ := Format(time.Date(2024, 1, d, 0, 0, 0, 0, time.UTC), "Do")
		want := map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 31: "31st"}[d]
		if got != want {
			t.Errorf("Do(%d) = %q", d, got)
		}
	}
	for _, bad := range []string{"X", "x", "YYYY-ww", "kk", "SSS", "Z", "Mo", "gggg", "LL", "Y", "YYYYY", "HHH"} {
		var fe *FormatError
		if _, err := Format(when, bad); !errors.As(err, &fe) {
			t.Errorf("V-DAILY-01 Format(%q) accepted: %v", bad, err)
		}
	}
}

func TestExpand(t *testing.T) {
	// V-TPL-01, V-TPL-02
	v := Vars{Title: "My Note", Date: when, Time: when.Add(time.Hour)}
	tpl := "# {{title}}\n{{date}} {{ Time }} {{date:dddd}} {{time:YYYY}} {{TITLE}}\n{{other}} {{title:x}} <% tp.date.now() %> {{date"
	want := "# My Note\n2024-01-03 16:04 Wednesday 2024 My Note\n{{other}} {{title:x}} <% tp.date.now() %> {{date"
	got, err := Expand(tpl, v)
	if err != nil || got != want {
		t.Errorf("V-TPL-01 Expand = %q, %v", got, err)
	}
	v.DateFormat, v.TimeFormat = "DD.MM.YYYY", "h:mm A"
	if got, _ := Expand("{{date}} {{time}}", v); got != "03.01.2024 4:04 PM" {
		t.Errorf("V-TPL-01 configured formats = %q", got)
	}
	if _, err := Expand("{{date:X}}", v); err == nil {
		t.Error("V-TPL-01 unsupported token accepted")
	}
}

func openVault(t *testing.T, files map[string]string) (*vault.Vault, string) {
	t.Helper()
	dir := t.TempDir() // an empty vault: every setting at its default
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := vault.Open(context.Background(), dir, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v, dir
}

func TestOpenDaily(t *testing.T) {
	// V-DAILY-01, V-DAILY-02
	ctx := context.Background()
	v, dir := openVault(t, map[string]string{
		".obsidian/daily-notes.json": `{"folder":"Journal/","format":"YYYY/MM/YYYY-MM-DD","template":"Templates/Daily"}`,
		"Templates/Daily.md":         "# {{title}}\nDate: {{date:dddd}} ({{date}}) at {{time}}\n<% tp.file.title %>\n",
	})
	now := time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC)
	n, err := Open(ctx, v, when, now, vault.WriteOptions{DryRun: true})
	if err != nil || !n.Created || n.File.Path != "Journal/2024/01/2024-01-03.md" {
		t.Fatalf("dry run = %+v, %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Journal")); !os.IsNotExist(err) {
		t.Error("dry run created the note")
	}
	n, err = Open(ctx, v, when, now, vault.WriteOptions{})
	if err != nil || !n.Created {
		t.Fatal(err)
	}
	want := "# 2024-01-03\nDate: Wednesday (2024/01/2024-01-03) at 09:30\n<% tp.file.title %>\n"
	if got, _ := os.ReadFile(filepath.Join(dir, "Journal", "2024", "01", "2024-01-03.md")); string(got) != want {
		t.Errorf("V-DAILY-02 note = %q", got)
	}
	again, err := Open(ctx, v, when, now, vault.WriteOptions{})
	if err != nil || again.Created || again.File.ETag != n.File.ETag {
		t.Errorf("V-DAILY-02 reopen = %+v, %v", again, err)
	}
	// Defaults: vault root, YYYY-MM-DD, no template.
	v2, _ := openVault(t, nil)
	n, err = Open(ctx, v2, when, now, vault.WriteOptions{})
	if err != nil || n.File.Path != "2024-01-03.md" || len(n.File.Data) != 0 {
		t.Errorf("V-DAILY-01 defaults = %+v, %v", n, err)
	}
	v3, _ := openVault(t, map[string]string{".obsidian/daily-notes.json": `{"format":"x"}`})
	if _, err := Open(ctx, v3, when, now, vault.WriteOptions{}); err == nil {
		t.Error("V-DAILY-01 unsupported format accepted")
	}
	v4, _ := openVault(t, map[string]string{".obsidian/daily-notes.json": `{"template":"Missing"}`})
	if _, err := Open(ctx, v4, when, now, vault.WriteOptions{}); err == nil {
		t.Error("V-DAILY-02 missing template accepted")
	}
}

func TestTemplates(t *testing.T) {
	// V-TPL-01, V-TPL-02
	ctx := context.Background()
	v, dir := openVault(t, map[string]string{
		".obsidian/templates.json": `{"folder":"Tpl","dateFormat":"DD/MM/YYYY"}`,
		"Tpl/Meeting.md":           "---\ndate: \"{{date}}\"\n---\n# {{title}}\n{{time}} <%* tR += 'x' %>\n",
		"Tpl/Sub/Other.md":         "x\n",
	})
	list, err := ListTemplates(ctx, v)
	if err != nil || len(list) != 2 || list[0] != "Tpl/Meeting.md" {
		t.Errorf("ListTemplates = %q, %v", list, err)
	}
	if _, err := CreateFromTemplate(ctx, v, "Meeting", "Notes/Standup.md", when, vault.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	want := "---\ndate: \"03/01/2024\"\n---\n# Standup\n15:04 <%* tR += 'x' %>\n"
	if got, _ := os.ReadFile(filepath.Join(dir, "Notes", "Standup.md")); string(got) != want {
		t.Errorf("V-TPL-02 created = %q", got)
	}
	if _, err := CreateFromTemplate(ctx, v, "Tpl/Meeting.md", "Notes/Standup.md", when, vault.WriteOptions{}); err == nil {
		t.Error("create over an existing note accepted")
	}
	if _, err := CreateFromTemplate(ctx, v, "Nope", "Notes/X.md", when, vault.WriteOptions{}); err == nil {
		t.Error("missing template accepted")
	}
	v2, _ := openVault(t, nil)
	if _, err := ListTemplates(ctx, v2); !errors.Is(err, ErrNoTemplateFolder) {
		t.Errorf("no folder: %v", err)
	}
}
