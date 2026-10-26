package links

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
	"github.com/swiftj/obsidian-mcp/vault"
)

func openIndex(t *testing.T, dir string) *Index {
	t.Helper()
	v, err := vault.Open(context.Background(), dir, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return New(v, Options{})
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// resolveOutgoing returns how each link of src resolved: the path, or
// "ambiguous", or "".
func resolveOutgoing(t *testing.T, x *Index, src string) []string {
	t.Helper()
	refs, err := x.Outgoing(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range refs {
		switch {
		case len(r.Candidates) > 0:
			out = append(out, "ambiguous")
		default:
			out = append(out, r.Resolved)
		}
	}
	return out
}

func TestResolveProbes(t *testing.T) {
	// L-RES-01, L-RES-02, L-RES-03, L-RES-04: the D-006 cases.
	x := openIndex(t, testutil.CopyProbeVault(t))
	for src, want := range map[string][]string{
		// [[Tie]] [[a/Tie]] [[b/Tie]] [[pr01/a/Tie]] [[Tie.md]] [[Near]]
		"pr01/src.md":               {"ambiguous", "pr01/a/Tie.md", "pr01/b/Tie.md", "pr01/a/Tie.md", "ambiguous", "pr01/Near.md"},
		"pr01-root-src.md":          {"ambiguous", "pr01/Near.md", "pr01/a/Tie.md"},
		"pr01/a/src-a.md":           {"pr01/a/Tie.md", "pr01/Near.md"},
		"pr01/b/src-b.md":           {"pr01/b/Tie.md"},
		"pr01/c/src-c.md":           {"ambiguous", "pr01/c/Near.md"},
		"pr01/a/deep/src-a-deep.md": {"ambiguous"},
		// Case is ignored, and Markdown destinations are decoded.
		"pr02/src.md": {"pr02/Case Target.md", "pr02/Case Target.md", "pr02/Case Target.md", "pr02/Case Target.md", "pr02/Case Target.md"},
		// Aliases never resolve; a file named like an alias does.
		"pr03/src.md": {"", "", "", "pr03/Clash.md"},
	} {
		if got := resolveOutgoing(t, x, src); !slices.Equal(got, want) {
			t.Errorf("L-RES-02 %s resolved %q; want %q", src, got, want)
		}
	}
	ctx := context.Background()
	_, err := x.Resolve(ctx, "pr01-root-src.md", "[[Tie]]")
	var ae *AmbiguousError
	if !errors.As(err, &ae) || !slices.Equal(ae.Candidates, []string{"pr01/a/Tie.md", "pr01/b/Tie.md"}) {
		t.Errorf("L-RES-06 ambiguous resolve = %v", err)
	}
	for link, want := range map[string]string{
		"Near":              "pr01/Near.md",
		"[[NEAR#Heading]]":  "pr01/Near.md",
		"[x](Near.md)":      "pr01/Near.md",
		"pr01/a/Tie.md":     "pr01/a/Tie.md",
		"Nick Name":         "",
		"[[#Heading only]]": "pr01/src.md",
	} {
		got, err := x.Resolve(ctx, "pr01/src.md", link)
		if err != nil || got != want {
			t.Errorf("L-RES-06 Resolve(%q) = %q, %v; want %q", link, got, err, want)
		}
	}
}

func TestIndexContexts(t *testing.T) {
	// L-IDX-01, L-BACK-01: where links count (D-006 pr14).
	x := openIndex(t, testutil.CopyProbeVault(t))
	refs, err := x.Backlinks(context.Background(), "pr14/Target14.md")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range refs {
		got = append(got, r.Source)
	}
	want := []string{
		"pr14/in-block-comment.md", "pr14/in-callout.md", "pr14/in-embed.md",
		"pr14/in-frontmatter-list.md", "pr14/in-frontmatter-quoted.md", "pr14/in-html-inline.md",
		"pr14/in-inline-comment.md", "pr14/in-mdlink.md", "pr14/in-normal.md", "pr14/in-table.md",
	}
	if !slices.Equal(got, want) {
		t.Errorf("L-IDX-01 backlinks of Target14 = %q; want %q", got, want)
	}
	for _, r := range refs {
		switch r.Source {
		case "pr14/in-frontmatter-list.md":
			if r.Kind != KindFrontmatter || r.Property != "rels.0" || r.Line != 3 {
				t.Errorf("L-IDX-01 frontmatter ref = %+v", r)
			}
		case "pr14/in-embed.md":
			if !r.Link.Embed || r.Line != 1 {
				t.Errorf("L-IDX-01 embed ref = %+v", r)
			}
		case "pr14/in-block-comment.md":
			if !r.Link.InComment || r.Line != 2 {
				t.Errorf("L-IDX-01 comment ref = %+v", r)
			}
		}
	}
}

func TestCanvasBaseAndMarkdownPaths(t *testing.T) {
	// L-IDX-01, L-BACK-01, L-RES-05
	dir := testutil.CopyVault(t, "basic")
	write(t, dir, "Map.canvas", `{"nodes":[{"id":"n1","type":"file","file":"Other.md","x":0,"y":0,"width":1,"height":1},`+
		`{"id":"n2","type":"text","text":"[[Welcome]]","x":0,"y":0,"width":1,"height":1}]}`)
	write(t, dir, "Bad.canvas", `{not json`)
	write(t, dir, "Folder/Uses.md", "![[Tasks.base#View]]\n"+
		"[a](./Sub/Deep.md) [b](../Other.md) [c](/Welcome.md) [d](<../Welcome.md>)\n"+
		"[e](Deep.md) [f](../../Escape.md) [g](https://example.com/x.md) [h](Nope.md)\n")
	x := openIndex(t, dir)
	ctx := context.Background()
	back, err := x.Backlinks(ctx, "Other.md")
	if err != nil {
		t.Fatal(err)
	}
	var canvas, md bool
	for _, r := range back {
		canvas = canvas || (r.Source == "Map.canvas" && r.Kind == KindCanvas && r.Node == "n1")
		md = md || r.Source == "Folder/Uses.md"
	}
	if !canvas || !md {
		t.Errorf("L-BACK-01 backlinks of Other = %+v", back)
	}
	if back, _ := x.Backlinks(ctx, "Tasks.base"); len(back) != 1 || !back[0].Link.Embed || back[0].Link.Subpath != "#View" {
		t.Errorf("L-IDX-01 .base embed = %+v", back)
	}
	got := resolveOutgoing(t, x, "Folder/Uses.md")
	want := []string{"Tasks.base", "Folder/Sub/Deep.md", "Other.md", "Welcome.md", "Welcome.md", "Folder/Sub/Deep.md", "", ""}
	if !slices.Equal(got, want) {
		t.Errorf("L-RES-05 markdown links resolved %q; want %q", got, want)
	}
	// Text nodes are not file links.
	if out, _ := x.Outgoing(ctx, "Map.canvas"); len(out) != 1 {
		t.Errorf("L-IDX-01 canvas refs = %+v", out)
	}
	if _, err := x.Backlinks(ctx, "Missing.md"); !errors.As(err, new(*vault.NotFoundError)) {
		t.Errorf("backlinks of a missing file: %v", err)
	}
	if _, err := x.Backlinks(ctx, "../x.md"); !errors.As(err, new(*vault.PathError)) {
		t.Errorf("backlinks of a bad path: %v", err)
	}
}

func TestUnresolvedOrphansExcluded(t *testing.T) {
	// L-UNRES-01, L-ORPH-01, V-SRCH-03
	dir := testutil.CopyVault(t, "basic")
	write(t, dir, ".obsidian/app.json", `{"userIgnoreFilters":["Archive/"]}`)
	write(t, dir, "Archive/Old.md", "[[Gone]] and [[Lonely]]\n")
	write(t, dir, "Lonely.md", "Only linked from the archive. [[Lonely]]\n")
	write(t, dir, "Dangling.md", "x\n\n[[Missing note]] [[Welcome]]\n")
	x := openIndex(t, dir)
	ctx := context.Background()
	un, err := x.Unresolved(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(un) != 1 || un[0].Source != "Dangling.md" || un[0].Line != 3 || un[0].Link.Target != "Missing note" {
		t.Errorf("L-UNRES-01 unresolved = %+v", un)
	}
	orph, err := x.Orphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Lonely is linked from an excluded file, which still counts; its self
	// link does not. Archive/Old.md is excluded, so it is not listed.
	if slices.Contains(orph, "Lonely.md") || slices.Contains(orph, "Archive/Old.md") || !slices.Contains(orph, "Dangling.md") ||
		slices.Contains(orph, "Welcome.md") || !slices.Contains(orph, "img.png") {
		t.Errorf("L-ORPH-01 orphans = %q", orph)
	}
	if back, _ := x.Backlinks(ctx, "Lonely.md"); len(back) != 1 || back[0].Source != "Archive/Old.md" {
		t.Errorf("V-SRCH-03 backlinks from an excluded file = %+v", back)
	}
}

func TestIncremental(t *testing.T) {
	// V-SRCH-06: the index follows edits, additions and deletions.
	dir := testutil.CopyVault(t, "basic")
	write(t, dir, "A/src.md", "[[Dup]]\n")
	write(t, dir, "B/Dup.md", "b\n")
	x := openIndex(t, dir)
	if got := resolveOutgoing(t, x, "A/src.md"); !slices.Equal(got, []string{"B/Dup.md"}) {
		t.Fatalf("before = %q", got)
	}
	reads := x.sc.Reads
	// A new same-named file in the source's folder takes over.
	write(t, dir, "A/Dup.md", "a\n")
	if got := resolveOutgoing(t, x, "A/src.md"); !slices.Equal(got, []string{"A/Dup.md"}) {
		t.Errorf("after add = %q", got)
	}
	if x.sc.Reads != reads+1 {
		t.Errorf("V-SRCH-06 refresh read %d files; want 1", x.sc.Reads-reads)
	}
	// Editing the source changes its links.
	write(t, dir, "A/src.md", "[[Welcome]]\n")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "A", "src.md"), future, future); err != nil {
		t.Fatal(err)
	}
	if got := resolveOutgoing(t, x, "A/src.md"); !slices.Equal(got, []string{"Welcome.md"}) {
		t.Errorf("after edit = %q", got)
	}
	// Deleting the target leaves the link unresolved.
	if err := os.Remove(filepath.Join(dir, "Welcome.md")); err != nil {
		t.Fatal(err)
	}
	if got := resolveOutgoing(t, x, "A/src.md"); !slices.Equal(got, []string{""}) {
		t.Errorf("after delete = %q", got)
	}
}

func TestLargeVault(t *testing.T) {
	// V-SRCH-06: 10k made-up notes index inside the call timeout.
	if testing.Short() {
		t.Skip("large vault")
	}
	dir := t.TempDir()
	for i := range 10000 {
		var sb strings.Builder
		fmt.Fprintf(&sb, "# Note %d\n\n", i)
		for j := range 20 {
			fmt.Fprintf(&sb, "Line %d links [[Note %d]] and [[F%02d/Note %d#H]].\n", j, (i+j+1)%10000, (i+j)%100, (i*3+j)%10000)
		}
		write(t, dir, fmt.Sprintf("F%02d/Note %d.md", i%100, i), sb.String())
	}
	x := openIndex(t, dir)
	start := time.Now()
	back, err := x.Backlinks(context.Background(), "F05/Note 5.md")
	took := time.Since(start)
	if err != nil || len(back) == 0 {
		t.Fatalf("V-SRCH-06 backlinks = %d, %v", len(back), err)
	}
	if took > 30*time.Second {
		t.Errorf("V-SRCH-06 first build took %v", took)
	}
	start = time.Now()
	if _, err := x.Orphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Logf("10k notes: build+query %v, warm query %v", took, time.Since(start))
}
