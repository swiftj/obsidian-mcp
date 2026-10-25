package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalRecords(t *testing.T) {
	// V-JRN-01: every change keeps the previous bytes outside the vault.
	ctx := context.Background()
	jdir := filepath.Join(t.TempDir(), "journal")
	j, err := OpenJournal(jdir, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	if j.cap != DefaultJournalCap || DefaultJournalCap != 500<<20 {
		t.Errorf("V-JRN-02 default cap = %d", j.cap)
	}
	v, dir := openBasic(t, Options{Journal: j})
	if strings.HasPrefix(jdir, dir) {
		t.Fatal("journal inside the vault")
	}

	f := mustRead(t, v, "Other.md")
	if _, err := v.Replace(ctx, "Other.md", []byte("v2\n"), f.ETag, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Create(ctx, "New.md", []byte("n\n"), WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	w := mustRead(t, v, "Welcome.md")
	if _, err := v.Trash(ctx, "Welcome.md", w.ETag, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Replace(ctx, "Other.md", []byte("dry\n"), ETag([]byte("v2\n")), WriteOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}

	ents, err := j.Entries(v)
	if err != nil {
		t.Fatal(err)
	}
	var ops []string
	for _, e := range ents {
		ops = append(ops, e.Op+":"+e.Path)
	}
	if strings.Join(ops, " ") != "trash:Welcome.md create:New.md replace:Other.md" {
		t.Fatalf("V-JRN-01 entries (newest first) = %v", ops)
	}
	if got, err := j.Data(v, ents[2].ID); err != nil || string(got) != string(f.Data) {
		t.Errorf("V-JRN-01 replace kept %q, %v; want %q", got, err, f.Data)
	}
	if got, err := j.Data(v, ents[0].ID); err != nil || string(got) != string(w.Data) {
		t.Errorf("V-JRN-01 trash kept %q, %v", got, err)
	}
	if ents[0].NewPath != ".trash/Welcome.md" || ents[2].ETagBefore != f.ETag || ents[1].HasData {
		t.Errorf("V-JRN-01 entry metadata = %+v", ents)
	}
	if _, err := j.Data(v, "../x"); err == nil {
		t.Error("journal Data accepted a path")
	}

	// A second vault with the same folder name keeps its own entries.
	other := filepath.Join(t.TempDir(), filepath.Base(dir))
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	v2, err := Open(ctx, other, Options{Journal: j})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v2.Close() }()
	if e2, _ := j.Entries(v2); len(e2) != 0 {
		t.Errorf("V-JRN-01 entries leaked between vaults: %v", e2)
	}
}

func TestJournalPrune(t *testing.T) {
	// V-JRN-02: past the cap, the oldest entries are pruned first.
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "j"), 3000)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Close() }()
	v, _ := openBasic(t, Options{Journal: j})
	body := []byte(strings.Repeat("x", 1000) + "\n")
	tag := mustRead(t, v, "Other.md").ETag
	for i := range 6 {
		next := append([]byte{byte('a' + i)}, body...)
		if _, err := v.Replace(ctx, "Other.md", next, tag, WriteOptions{}); err != nil {
			t.Fatal(err)
		}
		tag = ETag(next)
	}
	ents, err := j.Entries(v)
	if err != nil {
		t.Fatal(err)
	}
	dirs, _ := j.entryDirs("")
	var total int64
	for _, d := range dirs {
		total += d.size
	}
	if total > 3000 || len(ents) == 0 || len(ents) >= 6 {
		t.Fatalf("V-JRN-02 %d entries, %d bytes after pruning (cap 3000)", len(ents), total)
	}
	newest, _ := j.Data(v, ents[0].ID)
	if newest[0] != 'e' {
		t.Errorf("V-JRN-02 newest entry kept %q; want the bytes before the last write", newest[:1])
	}
}

func TestDefaultJournalDir(t *testing.T) {
	// V-JRN-01: the default lives in ~/.local/state/obs-mcp/journal.
	d, err := DefaultJournalDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(d, filepath.Join(".local", "state", "obs-mcp", "journal")) {
		t.Errorf("V-JRN-01 default journal dir = %s", d)
	}
}
