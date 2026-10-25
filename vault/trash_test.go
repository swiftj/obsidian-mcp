package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTrashRoundTrip(t *testing.T) {
	ctx := context.Background()
	v, dir := openBasic(t, Options{})
	f := mustRead(t, v, "Folder/Sub/Deep.md")

	// V-TRASH-01: the file moves to .trash/<same path>.
	if _, err := v.Trash(ctx, "Folder/Sub/Deep.md", "wrong", WriteOptions{}); err == nil {
		t.Error("V-WRITE-01 trash with a stale etag succeeded")
	}
	res, err := v.Trash(ctx, "Folder/Sub/Deep.md", f.ETag, WriteOptions{})
	if err != nil || res.Path != ".trash/Folder/Sub/Deep.md" {
		t.Fatalf("V-TRASH-01 trash = %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Folder/Sub/Deep.md")); !errors.Is(err, os.ErrNotExist) {
		t.Error("V-TRASH-01 file still in place")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".trash/Folder/Sub/Deep.md")); string(b) != string(f.Data) {
		t.Errorf("V-TRASH-01 trashed bytes = %q", b)
	}

	// V-TRASH-01: a collision adds ~N before the extension.
	if _, err := v.Create(ctx, "Folder/Sub/Deep.md", []byte("second\n"), WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	g := mustRead(t, v, "Folder/Sub/Deep.md")
	res, err = v.Trash(ctx, "Folder/Sub/Deep.md", g.ETag, WriteOptions{})
	if err != nil || res.Path != ".trash/Folder/Sub/Deep~1.md" {
		t.Fatalf("V-TRASH-01 collision = %+v, %v", res, err)
	}

	// V-TRASH-03: entries are listed with their original paths.
	got := map[string]string{}
	for e, err := range v.ListTrash(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		got[e.TrashPath] = e.OriginalPath
	}
	want := map[string]string{
		"Old.md":               "Old.md",
		"Folder/Sub/Deep.md":   "Folder/Sub/Deep.md",
		"Folder/Sub/Deep~1.md": "Folder/Sub/Deep.md",
	}
	if len(got) != len(want) {
		t.Errorf("V-TRASH-03 list = %v; want %v", got, want)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("V-TRASH-03 %s → %q; want %q", k, got[k], w)
		}
	}

	// V-TRASH-02: restore puts it back, and refuses an occupied path.
	res, err = v.Restore(ctx, "Folder/Sub/Deep.md", "", WriteOptions{})
	if err != nil || res.Path != "Folder/Sub/Deep.md" {
		t.Fatalf("V-TRASH-02 restore = %+v, %v", res, err)
	}
	if string(mustRead(t, v, "Folder/Sub/Deep.md").Data) != string(f.Data) {
		t.Error("V-TRASH-02 restored bytes differ")
	}
	var ce *ConflictError
	if _, err := v.Restore(ctx, "Folder/Sub/Deep~1.md", "", WriteOptions{}); !errors.As(err, &ce) {
		t.Errorf("V-TRASH-02 restore onto an occupied path: %v", err)
	}
	if res, err := v.Restore(ctx, "Folder/Sub/Deep~1.md", "Folder/Sub/Deep 2.md", WriteOptions{}); err != nil || res.Path != "Folder/Sub/Deep 2.md" {
		t.Errorf("V-TRASH-02 restore to another path = %+v, %v", res, err)
	}
	if _, err := v.Restore(ctx, "Missing.md", "", WriteOptions{}); !isNotFound(err) {
		t.Errorf("V-TRASH-02 restore missing: %v", err)
	}
	_, err = v.Restore(ctx, "../Other.md", "", WriteOptions{})
	asPathError(t, err, "restore traversal")
	_, err = v.Restore(ctx, "Old.md", ".obsidian/Old.md", WriteOptions{})
	asPathError(t, err, "restore into config")
}

func TestOriginalPath(t *testing.T) {
	// V-TRASH-03
	for in, want := range map[string]string{
		"a.md": "a.md", "a~1.md": "a.md", "d/a~12.md": "d/a.md", "a~x.md": "a~x.md",
		"~1.md": "~1.md", "noext~3": "noext", "a~1.b~2.md": "a~1.b.md",
	} {
		if got := originalPath(in); got != want {
			t.Errorf("V-TRASH-03 originalPath(%q) = %q; want %q", in, got, want)
		}
	}
}
