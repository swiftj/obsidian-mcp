package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
)

func openBasic(t *testing.T, opt Options) (*Vault, string) {
	t.Helper()
	dir := testutil.CopyVault(t, "basic")
	v, err := Open(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v, dir
}

func listPaths(t *testing.T, v *Vault, dir string, recursive bool) []string {
	t.Helper()
	var out []string
	for e, err := range v.List(context.Background(), dir, recursive) {
		if err != nil {
			t.Fatalf("list %q: %v", dir, err)
		}
		out = append(out, e.Path)
	}
	return out
}

func mustRead(t *testing.T, v *Vault, p string) *File {
	t.Helper()
	f, err := v.Read(context.Background(), p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return f
}

func asPathError(t *testing.T, err error, what string) {
	t.Helper()
	var pe *PathError
	if !errors.As(err, &pe) {
		t.Errorf("%s: err = %v; want *PathError", what, err)
	}
}

func TestList(t *testing.T) {
	// V-LIST-01: listings are iter.Seq2 streams that skip hidden and denied paths.
	v, _ := openBasic(t, Options{})
	got := listPaths(t, v, "", true)
	want := []string{"Board.canvas", "Doc.pdf", "Folder", "Folder/Sub", "Folder/Sub/Deep.md",
		"Other.md", "Tasks.base", "Welcome.md", "Windows.md", "img.png"}
	if !slices.Equal(got, want) {
		t.Errorf("V-LIST-01 recursive list = %q\nwant %q", got, want)
	}
	if got := listPaths(t, v, "Folder", false); !slices.Equal(got, []string{"Folder/Sub"}) {
		t.Errorf("V-LIST-01 non-recursive list = %q", got)
	}
	n := 0
	for _, err := range v.List(context.Background(), "", true) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		if n == 2 {
			break
		}
	}
	for _, bad := range []string{".obsidian", "node_modules", "Welcome.md", "Missing", "../x"} {
		for _, err := range v.List(context.Background(), bad, false) {
			if err == nil {
				t.Errorf("V-LIST-01 list %q yielded an entry", bad)
			}
		}
	}

	dv, _ := openBasic(t, Options{Deny: []string{"Folder/**", "*.pdf"}})
	got = listPaths(t, dv, "", true)
	for _, p := range got {
		if strings.HasPrefix(p, "Folder") || strings.HasSuffix(p, ".pdf") {
			t.Errorf("V-PATH-09 denied path listed: %s", p)
		}
	}
}

func TestKindOf(t *testing.T) {
	// V-LIST-02
	tests := map[string]Kind{
		"a.md": KindMarkdown, "a.canvas": KindCanvas, "a.base": KindBase,
		"a.avif": KindImage, "a.bmp": KindImage, "a.gif": KindImage, "a.jpeg": KindImage, "a.jpg": KindImage,
		"a.PNG": KindImage, "a.svg": KindImage, "a.webp": KindImage,
		"a.flac": KindAudio, "a.m4a": KindAudio, "a.mp3": KindAudio, "a.ogg": KindAudio, "a.wav": KindAudio, "a.3gp": KindAudio,
		"a.mkv": KindVideo, "a.mov": KindVideo, "a.mp4": KindVideo, "a.ogv": KindVideo, "a.webm": KindVideo,
		"a.pdf": KindPDF, "a.txt": KindOther, "noext": KindOther,
	}
	for name, want := range tests {
		if got := KindOf(name); got != want {
			t.Errorf("V-LIST-02 KindOf(%q) = %s; want %s", name, got, want)
		}
	}
}

func TestRead(t *testing.T) {
	// V-READ-01: every read returns the lowercase hex sha256 of the bytes.
	v, dir := openBasic(t, Options{})
	f := mustRead(t, v, "Windows.md")
	raw, err := os.ReadFile(filepath.Join(dir, "Windows.md"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if f.ETag != hex.EncodeToString(sum[:]) || !bytes.Equal(f.Data, raw) || f.Kind != KindMarkdown {
		t.Errorf("V-READ-01 read = %+v", f)
	}
	var nf *NotFoundError
	if _, err := v.Read(context.Background(), "Missing.md"); !errors.As(err, &nf) {
		t.Errorf("V-ERR-01 missing file: %v; want *NotFoundError", err)
	}
	_, err = v.Read(context.Background(), "Folder")
	asPathError(t, err, "read folder")
}

func TestCreate(t *testing.T) {
	ctx := context.Background()
	v, dir := openBasic(t, Options{})
	res, err := v.Create(ctx, "New/Deeper/Note.md", []byte("hello\n"), WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, v, "New/Deeper/Note.md"); got.ETag != res.ETag || string(got.Data) != "hello\n" {
		t.Errorf("created file = %q, etag %s; result etag %s", got.Data, got.ETag, res.ETag)
	}
	// V-WRITE-02: creating an existing file fails.
	var ce *ConflictError
	_, err = v.Create(ctx, "Welcome.md", []byte("x"), WriteOptions{})
	if !errors.As(err, &ce) || ce.Expected != "" || ce.Current != mustRead(t, v, "Welcome.md").ETag {
		t.Errorf("V-WRITE-02 create existing: %v", err)
	}
	// V-WRITE-08: new names must work as link targets.
	for _, bad := range []string{"a#b.md", "Bad: name.md", "x|y.md", "a^b.md", "a%%b.md", "a[[b.md", "F:older/n.md", "F[[x/n.md"} {
		_, err := v.Create(ctx, bad, []byte("x"), WriteOptions{})
		asPathError(t, err, "V-WRITE-08 create "+bad)
	}
	if _, err := os.Stat(filepath.Join(dir, "F:older")); !errors.Is(err, os.ErrNotExist) {
		t.Error("V-WRITE-08 refused folder was created")
	}
	// V-PATH-10: only .md, .canvas and .base can be written.
	for _, bad := range []string{"x.png", "x.txt", "x.js", ".obsidian/x.md", ".obsidian/plugins/p/main.js"} {
		_, err := v.Create(ctx, bad, []byte("x"), WriteOptions{})
		asPathError(t, err, "V-PATH-10 create "+bad)
	}
	for _, ok := range []string{"n.canvas", "n.base"} {
		if _, err := v.Create(ctx, ok, []byte("{}"), WriteOptions{}); err != nil {
			t.Errorf("V-PATH-10 create %s: %v", ok, err)
		}
	}
}

func TestReplaceETag(t *testing.T) {
	// V-WRITE-01: a change needs the current etag; a mismatch is a ConflictError.
	ctx := context.Background()
	v, dir := openBasic(t, Options{})
	f := mustRead(t, v, "Other.md")
	if _, err := v.Replace(ctx, "Other.md", []byte("x\n"), "", WriteOptions{}); !errors.Is(err, ErrETagRequired) {
		t.Errorf("V-WRITE-01 empty etag: %v", err)
	}
	// Obsidian (or anything else) writes the file after our read.
	if err := os.WriteFile(filepath.Join(dir, "Other.md"), []byte("changed outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	_, err := v.Replace(ctx, "Other.md", []byte("mine\n"), f.ETag, WriteOptions{})
	if !errors.As(err, &ce) || ce.Expected != f.ETag || ce.Current != ETag([]byte("changed outside\n")) {
		t.Fatalf("V-WRITE-01 stale etag: %v", err)
	}
	res, err := v.Replace(ctx, "Other.md", []byte("mine\n"), ce.Current, WriteOptions{})
	if err != nil || mustRead(t, v, "Other.md").ETag != res.ETag {
		t.Fatalf("V-WRITE-01 replace with fresh etag: %v", err)
	}
	if _, err := v.Replace(ctx, "Missing.md", []byte("x"), res.ETag, WriteOptions{}); !isNotFound(err) {
		t.Errorf("replace missing: %v", err)
	}
}

func TestReplaceAtomic(t *testing.T) {
	// V-WRITE-03: replacement keeps the mode and leaves no temp files.
	ctx := context.Background()
	v, dir := openBasic(t, Options{})
	p := filepath.Join(dir, "Other.md")
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(p)
	f := mustRead(t, v, "Other.md")
	if _, err := v.Replace(ctx, "Other.md", []byte("new\n"), f.ETag, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	if after.Mode().Perm() != 0o600 {
		t.Errorf("V-WRITE-03 mode = %v; want 0600", after.Mode().Perm())
	}
	if os.SameFile(before, after) {
		t.Error("V-WRITE-03 file was rewritten in place, not replaced by rename")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".obs-mcp-") {
			t.Errorf("V-WRITE-03 temp file left behind: %s", e.Name())
		}
	}
}

func TestConcurrentWrites(t *testing.T) {
	// V-WRITE-04: writes are serialised, so no two replaces succeed from the
	// same etag (no lost update). Run with -race too.
	ctx := context.Background()
	v, _ := openBasic(t, Options{})
	var mu sync.Mutex
	won := map[string]int{}
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			for j := range 10 {
				f, err := v.Read(ctx, "Other.md")
				if err != nil {
					t.Error(err)
					return
				}
				_, err = v.Replace(ctx, "Other.md", []byte(strings.Repeat("x", i*10+j+1)), f.ETag, WriteOptions{})
				var ce *ConflictError
				switch {
				case err == nil:
					mu.Lock()
					won[f.ETag]++
					mu.Unlock()
				case !errors.As(err, &ce):
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	for tag, n := range won {
		if n > 1 {
			t.Errorf("V-WRITE-04 %d replaces succeeded from etag %s", n, tag)
		}
	}
	if len(won) == 0 {
		t.Error("V-WRITE-04 no replace succeeded")
	}
}

func TestMatchStyle(t *testing.T) {
	// V-WRITE-05: line endings and BOM follow the existing file.
	tests := []struct{ old, data, want string }{
		{"a\nb\n", "x\r\ny\r\n", "x\ny\n"},
		{"a\r\nb\r\n", "x\ny\n", "x\r\ny\r\n"},
		{"a\r\nb\r\n", "x\r\ny\n", "x\r\ny\r\n"},
		{"\ufeffa\r\n", "x\n", "\ufeffx\r\n"},
		{"a\n", "\ufeffx\n", "x\n"},
		{"", "x\r\n", "x\n"},
		{"no newline", "x\ny", "x\ny"},
	}
	for _, tt := range tests {
		if got := string(MatchStyle([]byte(tt.old), []byte(tt.data))); got != tt.want {
			t.Errorf("V-WRITE-05 MatchStyle(%q, %q) = %q; want %q", tt.old, tt.data, got, tt.want)
		}
	}
	ctx := context.Background()
	v, _ := openBasic(t, Options{})
	f := mustRead(t, v, "Windows.md")
	if _, err := v.Replace(ctx, "Windows.md", []byte("# Windows\n\nEdited.\n"), f.ETag, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, v, "Windows.md").Data); got != "\ufeff# Windows\r\n\r\nEdited.\r\n" {
		t.Errorf("V-WRITE-05 CRLF+BOM file after replace = %q", got)
	}
}

func TestInvalidUTF8(t *testing.T) {
	// V-WRITE-06
	ctx := context.Background()
	v, _ := openBasic(t, Options{})
	bad := []byte("bad \xff\xfe bytes\n")
	_, err := v.Create(ctx, "Bad.md", bad, WriteOptions{})
	asPathError(t, err, "V-WRITE-06 create")
	f := mustRead(t, v, "Other.md")
	_, err = v.Replace(ctx, "Other.md", bad, f.ETag, WriteOptions{})
	asPathError(t, err, "V-WRITE-06 replace")
}

func TestDryRun(t *testing.T) {
	// V-WRITE-07: a dry run returns a diff and leaves the disk byte-identical.
	ctx := context.Background()
	v, dir := openBasic(t, Options{})
	before := testutil.TreeSums(t, dir)
	dry := WriteOptions{DryRun: true}
	f := mustRead(t, v, "Welcome.md")
	res, err := v.Replace(ctx, "Welcome.md", []byte("# Welcome\n\nChanged.\n"), f.ETag, dry)
	if err != nil || !res.DryRun || !strings.Contains(res.Diff, "+Changed.") || !strings.Contains(res.Diff, "--- a/Welcome.md") {
		t.Errorf("V-WRITE-07 replace dry run = %+v, %v", res, err)
	}
	if res, err := v.Create(ctx, "Dry/New.md", []byte("x\n"), dry); err != nil || !strings.Contains(res.Diff, "+x") {
		t.Errorf("V-WRITE-07 create dry run = %+v, %v", res, err)
	}
	if res, err := v.Trash(ctx, "Welcome.md", f.ETag, dry); err != nil || res.Path != ".trash/Welcome.md" {
		t.Errorf("V-WRITE-07 trash dry run = %+v, %v", res, err)
	}
	if res, err := v.Restore(ctx, "Old.md", "", dry); err != nil || res.Path != "Old.md" {
		t.Errorf("V-WRITE-07 restore dry run = %+v, %v", res, err)
	}
	if after := testutil.TreeSums(t, dir); !maps.Equal(before, after) {
		t.Errorf("V-WRITE-07 dry runs changed the disk:\nbefore %v\nafter  %v", before, after)
	}
}

func TestNoLockFile(t *testing.T) {
	// V-LOCK-01: writes create nothing but their targets and .trash entries.
	ctx := context.Background()
	v, dir := openBasic(t, Options{})
	before := testutil.TreeSums(t, dir)
	if _, err := v.Create(ctx, "Lock.md", []byte("x\n"), WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	f := mustRead(t, v, "Other.md")
	if _, err := v.Replace(ctx, "Other.md", []byte("y\n"), f.ETag, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	after := testutil.TreeSums(t, dir)
	for p := range after {
		if _, ok := before[p]; !ok && p != "Lock.md" {
			t.Errorf("V-LOCK-01 unexpected new path %s", p)
		}
	}
}

func TestDenyHidesEverywhere(t *testing.T) {
	// V-PATH-09: a denied path can't be read or written.
	ctx := context.Background()
	v, _ := openBasic(t, Options{Deny: []string{"Folder/**", "Other.md"}})
	_, err := v.Read(ctx, "Folder/Sub/Deep.md")
	asPathError(t, err, "V-PATH-09 read")
	_, err = v.Create(ctx, "Folder/New.md", []byte("x"), WriteOptions{})
	asPathError(t, err, "V-PATH-09 create")
	_, err = v.Replace(ctx, "other.md", []byte("x"), "tag", WriteOptions{})
	asPathError(t, err, "V-PATH-09 replace")
}
