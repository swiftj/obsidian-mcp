package vault

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
)

func openJournaled(t *testing.T) (*Vault, string) {
	t.Helper()
	j, err := OpenJournal(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return openBasic(t, Options{Journal: j})
}

func sampleSet(t *testing.T, v *Vault) []Change {
	t.Helper()
	w := mustRead(t, v, "Welcome.md")
	o := mustRead(t, v, "Other.md")
	img := mustRead(t, v, "img.png")
	return []Change{
		{Kind: ChangeWrite, Path: "Welcome.md", ETag: w.ETag, Data: []byte("# Welcome\n\nNow links to [[Moved]].\n")},
		{Kind: ChangeMove, Path: "Other.md", To: "New/Deep/Moved.md", ETag: o.ETag},
		{Kind: ChangeMove, Path: "img.png", To: "New/img.png", ETag: img.ETag},
		{Kind: ChangeCreate, Path: "New/Fresh.md", Data: []byte("fresh\n")},
	}
}

func TestApply(t *testing.T) {
	// L-REN-06
	ctx := context.Background()
	v, dir := openJournaled(t)
	before := testutil.TreeSums(t, dir)
	res, err := v.Apply(ctx, sampleSet(t, v), WriteOptions{DryRun: true})
	if err != nil || len(res.Results) != 4 || !strings.Contains(res.Diff(), "rename Other.md → New/Deep/Moved.md") || !strings.Contains(res.Diff(), "+fresh") {
		t.Fatalf("L-REN-05 dry run = %+v, %v", res, err)
	}
	if !maps.Equal(before, testutil.TreeSums(t, dir)) {
		t.Fatal("V-WRITE-07 dry run changed the disk")
	}
	other := mustRead(t, v, "Other.md").Data
	if _, err := v.Apply(ctx, sampleSet(t, v), WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, v, "New/Deep/Moved.md").Data; string(got) != string(other) {
		t.Errorf("moved content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "Other.md")); !os.IsNotExist(err) {
		t.Error("source still exists after move")
	}
	// Undo reverses the whole set and restores the tree.
	u, err := v.Undo(ctx, WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Entries) != 4 {
		t.Errorf("V-JRN-03 undid %d entries", len(u.Entries))
	}
	after := testutil.TreeSums(t, dir)
	delete(after, ".trash/New") // the undone create went to .trash
	delete(after, ".trash/New/Fresh.md")
	if !maps.Equal(before, after) {
		t.Errorf("V-JRN-03 undo left the tree changed:\n%v\n%v", before, after)
	}
}

func TestApplyAllOrNothing(t *testing.T) {
	// L-REN-06: a failure part-way reverses the finished steps.
	ctx := context.Background()
	for fail := range 4 {
		v, dir := openJournaled(t)
		before := testutil.TreeSums(t, dir)
		set := sampleSet(t, v)
		v.testFail = func(i int) error {
			if i == fail {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := v.Apply(ctx, set, WriteOptions{}); err == nil || err.Error() != "injected" {
			t.Fatalf("fail at %d: err = %v", fail, err)
		}
		if !maps.Equal(before, testutil.TreeSums(t, dir)) {
			t.Errorf("L-REN-06 fail at %d left the tree changed", fail)
		}
		if es, _ := v.journal.Entries(v); len(es) != 0 {
			t.Errorf("L-REN-06 fail at %d left %d journal entries", fail, len(es))
		}
	}
}

func TestApplyConflicts(t *testing.T) {
	// L-REN-06: every check runs before any write.
	ctx := context.Background()
	v, dir := openJournaled(t)
	set := sampleSet(t, v)
	set[3].Path = "Folder/Sub/Deep.md" // exists
	before := testutil.TreeSums(t, dir)
	var ce *ConflictError
	if _, err := v.Apply(ctx, set, WriteOptions{}); !errors.As(err, &ce) {
		t.Fatalf("create over existing: %v", err)
	}
	set = sampleSet(t, v)
	set[1].ETag = "stale"
	if _, err := v.Apply(ctx, set, WriteOptions{}); !errors.As(err, &ce) {
		t.Fatalf("stale etag: %v", err)
	}
	if !maps.Equal(before, testutil.TreeSums(t, dir)) {
		t.Error("a refused set changed the disk")
	}
	for _, bad := range []Change{
		{Kind: ChangeMove, Path: "Welcome.md", To: "Welcome.txt", ETag: "x"},
		{Kind: ChangeMove, Path: "Welcome.md", To: ".obsidian/x.md", ETag: "x"},
		{Kind: ChangeMove, Path: "Welcome.md", To: "A/B.md"},
		{Kind: ChangeWrite, Path: "img.png", ETag: "x", Data: []byte("x")},
		{Kind: "bogus", Path: "x.md"},
	} {
		if _, err := v.Apply(ctx, []Change{bad}, WriteOptions{}); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	w := mustRead(t, v, "Welcome.md")
	if _, err := v.Apply(ctx, []Change{
		{Kind: ChangeWrite, Path: "Welcome.md", ETag: w.ETag, Data: []byte("x")},
		{Kind: ChangeMove, Path: "Welcome.md", To: "W2.md", ETag: w.ETag},
	}, WriteOptions{}); err == nil {
		t.Error("a path in two changes was accepted")
	}
}

func TestUndoSingleWrites(t *testing.T) {
	// V-JRN-03
	ctx := context.Background()
	v, dir := openJournaled(t)
	before := testutil.TreeSums(t, dir)
	f := mustRead(t, v, "Welcome.md")
	r, err := v.Replace(ctx, "Welcome.md", []byte("changed\n"), f.ETag, WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Create(ctx, "N/New.md", []byte("new\n"), WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	o := mustRead(t, v, "Other.md")
	tr, err := v.Trash(ctx, "Other.md", o.ETag, WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Restore(ctx, strings.TrimPrefix(tr.Path, ".trash/"), "", WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	// A dry run shows the diff and changes nothing.
	mid := testutil.TreeSums(t, dir)
	u, err := v.Undo(ctx, WriteOptions{DryRun: true})
	if err != nil || !strings.Contains(u.Diff(), "rename Other.md → .trash/Other.md") || !maps.Equal(mid, testutil.TreeSums(t, dir)) {
		t.Fatalf("V-JRN-03 dry run = %+v, %v", u, err)
	}
	for range 4 { // restore, trash, create, replace
		if _, err := v.Undo(ctx, WriteOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	after := testutil.TreeSums(t, dir)
	for k := range after {
		if strings.HasPrefix(k, ".trash/N") {
			delete(after, k)
		}
	}
	if !maps.Equal(before, after) {
		t.Errorf("V-JRN-03 four undos left the tree changed")
	}
	if _, err := v.Undo(ctx, WriteOptions{}); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("undo with an empty journal: %v", err)
	}
	// An external edit after the write refuses its undo.
	f = mustRead(t, v, "Welcome.md")
	if _, err := v.Replace(ctx, "Welcome.md", []byte("again\n"), f.ETag, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Welcome.md"), []byte("edited in Obsidian\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if _, err := v.Undo(ctx, WriteOptions{}); !errors.As(err, &ce) {
		t.Errorf("V-JRN-03 undo over an external edit: %v (want conflict, %v)", err, r)
	}
	nj, _ := openBasic(t, Options{})
	if _, err := nj.Undo(ctx, WriteOptions{}); !errors.Is(err, ErrNoJournal) {
		t.Errorf("undo without a journal: %v", err)
	}
}

func TestCreateFolderAndAttachment(t *testing.T) {
	// V-ATT-01
	ctx := context.Background()
	v, dir := openJournaled(t)
	if r, err := v.CreateFolder(ctx, "A/B", WriteOptions{}); err != nil || !r.Changed {
		t.Fatalf("CreateFolder = %+v, %v", r, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "A", "B")); err != nil || !fi.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
	if r, err := v.CreateFolder(ctx, "A/B", WriteOptions{}); err != nil || r.Changed {
		t.Errorf("CreateFolder existing = %+v, %v", r, err)
	}
	for _, bad := range []string{"Welcome.md", ".obsidian/x", "A/bad#name", ""} {
		if _, err := v.CreateFolder(ctx, bad, WriteOptions{}); err == nil {
			t.Errorf("CreateFolder(%q) accepted", bad)
		}
	}
	r, err := v.CreateAttachment(ctx, "img.png", []byte("png"), WriteOptions{})
	if err != nil || r.Path != "img 1.png" {
		t.Fatalf("attachment clash = %+v, %v", r, err)
	}
	if r, err := v.CreateAttachment(ctx, "Att/doc.pdf", []byte("pdf"), WriteOptions{}); err != nil || r.Path != "Att/doc.pdf" {
		t.Errorf("attachment in new folder = %+v, %v", r, err)
	}
	for _, bad := range []string{"x.md", "x.exe", ".obsidian/x.png"} {
		if _, err := v.CreateAttachment(ctx, bad, []byte("x"), WriteOptions{}); err == nil {
			t.Errorf("CreateAttachment(%q) accepted", bad)
		}
	}
	// Undo moves the new attachment to the trash.
	if _, err := v.Undo(ctx, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".trash", "Att", "doc.pdf")); err != nil {
		t.Errorf("undone attachment not in trash: %v", err)
	}
}
