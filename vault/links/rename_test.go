package links

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swiftj/obsidian-mcp/vault"
)

// corpus is a made-up vault with every link form pointing at
// Notes/Target.md, written in the shortest format.
var corpus = map[string]string{
	"Notes/Target.md": "# Top\n\n## Sub\n\npara ^b1\n\nSee [[#Sub]] and [[#Top#Sub|here]].\n",
	"Src.md": "---\nrel: \"[[Target]]\"\nrels:\n  - \"[[Target#Top]]\"\n  - \"[[Other]]\"\n---\n" +
		"[[Target]] [[Target#Top|t]] ![[Target#^b1]] [x](Target.md) [y](<Target.md#Sub>)\n" +
		"%%[[Target]]%% `[[Target]]` [[Target#Top#Sub]]\n\n```\n[[Target]]\n```\n",
	"Notes/Rel.md": "[a](Target.md) [[Src]] [[Other]]\n",
	"Other.md":     "plain\n",
	"Map.canvas":   "{\n\t\"nodes\":[\n\t\t{\"id\":\"n1\",\"type\":\"file\",\"file\":\"Notes/Target.md\",\"subpath\":\"#Sub\",\"x\":0,\"y\":0,\"width\":1,\"height\":1},\n\t\t{\"id\":\"n2\",\"type\":\"text\",\"text\":\"[[Target]]\",\"x\":0,\"y\":0,\"width\":1,\"height\":1}\n\t],\n\t\"edges\":[]\n}\n",
}

func corpusVault(t *testing.T, format string, extra map[string]string) (*vault.Vault, *Index, string) {
	t.Helper()
	dir := t.TempDir()
	files := maps.Clone(corpus)
	maps.Copy(files, extra)
	if format != "" {
		files[".obsidian/app.json"] = `{"newLinkFormat":"` + format + `"}`
	}
	for rel, content := range files {
		write(t, dir, rel, content)
	}
	j, err := vault.OpenJournal(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	v, err := vault.Open(context.Background(), dir, vault.Options{Journal: j})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v, New(v, Options{}), dir
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func applyPlan(t *testing.T, v *vault.Vault, p *Plan, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Apply(context.Background(), p.Changes, vault.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
}

func sums(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		rel, _ := filepath.Rel(dir, p)
		if err != nil || strings.HasPrefix(rel, ".obsidian") {
			return err
		}
		if d.IsDir() {
			out[rel] = "dir"
			return nil
		}
		b, err := os.ReadFile(p)
		out[rel] = string(b)
		return err
	})
	return out
}

func TestPlanMoveCorpus(t *testing.T) {
	// L-REN-01, L-REN-02, L-REN-03, L-REN-05
	ctx := context.Background()
	for _, tt := range []struct {
		format       string
		src, rel, fm string // expected forms of the moved target
	}{
		{"shortest", "Renamed", "Renamed.md", "Renamed"},
		{"absolute", "Archive/Renamed", "Archive/Renamed.md", "Archive/Renamed"},
		{"relative", "Archive/Renamed", "../Archive/Renamed.md", "Archive/Renamed"},
	} {
		v, x, dir := corpusVault(t, tt.format, nil)
		plan, err := x.PlanMoves(ctx, map[string]string{"Notes/Target.md": "Archive/Renamed.md"})
		if err != nil {
			t.Fatal(err)
		}
		md := strings.ReplaceAll(tt.src, " ", "%20") + ".md"
		applyPlan(t, v, plan, nil)
		wantSrc := "---\nrel: \"[[" + tt.fm + "]]\"\nrels:\n  - \"[[" + tt.fm + "#Top]]\"\n  - \"[[Other]]\"\n---\n" +
			"[[" + tt.src + "]] [[" + tt.src + "#Top|t]] ![[" + tt.src + "#^b1]] [x](" + md + ") [y](<" + md + "#Sub>)\n" +
			"%%[[" + tt.src + "]]%% `[[Target]]` [[" + tt.src + "#Top#Sub]]\n\n```\n[[Target]]\n```\n"
		if got := read(t, dir, "Src.md"); got != wantSrc {
			t.Errorf("L-REN-02 %s Src.md:\n%s\nwant:\n%s", tt.format, got, wantSrc)
		}
		if got := read(t, dir, "Notes/Rel.md"); got != "[a]("+tt.rel+") [[Src]] [[Other]]\n" {
			t.Errorf("L-REN-02 %s Rel.md = %q", tt.format, got)
		}
		if got := read(t, dir, "Map.canvas"); !strings.Contains(got, `"file":"Archive/Renamed.md","subpath":"#Sub"`) || !strings.Contains(got, `"text":"[[Target]]"`) {
			t.Errorf("L-REN-01 canvas = %s", got)
		}
		// The moved note's own links are unchanged; it now resolves as before.
		if got := read(t, dir, "Archive/Renamed.md"); got != corpus["Notes/Target.md"] {
			t.Errorf("moved note = %q", got)
		}
		back, err := x.Backlinks(ctx, "Archive/Renamed.md")
		if err != nil || len(back) != 11 {
			t.Errorf("L-REN-01 %s backlinks after move = %d, %v", tt.format, len(back), err)
		}
		if un, _ := x.Unresolved(ctx); len(un) != 0 {
			t.Errorf("L-REN-01 %s unresolved after move = %+v", tt.format, un)
		}
		if tt.format == "shortest" && (len(plan.Rewrites) != 11 || plan.Rewrites[0].Source != "Map.canvas") {
			t.Errorf("L-REN-05 rewrites = %+v", plan.Rewrites)
		}
	}
}

func TestMoveRoundTrip(t *testing.T) {
	// L-REN-07, L-REN-10
	ctx := context.Background()
	v, x, dir := corpusVault(t, "", nil)
	before := sums(t, dir)
	plan, err := x.PlanMoves(ctx, map[string]string{"Notes/Target.md": "Archive/Renamed.md"})
	applyPlan(t, v, plan, err)
	plan, err = x.PlanMoves(ctx, map[string]string{"Archive/Renamed.md": "Notes/Target.md"})
	applyPlan(t, v, plan, err)
	if after := sums(t, dir); !maps.Equal(before, after) {
		t.Errorf("L-REN-07 note round trip changed the vault:\n%v\n%v", before, after)
	}
	plan, err = x.PlanFolderMove(ctx, "Notes", "Deep/Moved")
	applyPlan(t, v, plan, err)
	if got := read(t, dir, "Deep/Moved/Rel.md"); got != corpus["Notes/Rel.md"] {
		t.Errorf("L-REN-10 moved folder note = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "Notes")); !os.IsNotExist(err) {
		t.Errorf("L-REN-10 old folder left behind: %v", err)
	}
	if got := read(t, dir, "Map.canvas"); !strings.Contains(got, `"file":"Deep/Moved/Target.md"`) {
		t.Errorf("L-REN-10 canvas after folder move = %s", got)
	}
	plan, err = x.PlanFolderMove(ctx, "Deep/Moved", "Notes")
	applyPlan(t, v, plan, err)
	if after := sums(t, dir); !maps.Equal(before, after) {
		t.Errorf("L-REN-07 folder round trip changed the vault")
	}
	// Errors.
	if _, err := x.PlanFolderMove(ctx, "Notes", "Notes/Inside"); err == nil {
		t.Error("L-REN-10 move into itself accepted")
	}
	if _, err := x.PlanFolderMove(ctx, "Notes", "Src.md"); err == nil {
		t.Error("L-REN-10 move onto a file accepted")
	}
	if _, err := x.PlanMoves(ctx, map[string]string{"Src.md": "Other.md"}); err == nil {
		t.Error("move over an existing file accepted")
	}
	if _, err := x.PlanMoves(ctx, map[string]string{"Src.md": "Src.txt"}); err == nil {
		t.Error("extension change accepted")
	}
}

func TestMoveAmbiguityAndRelative(t *testing.T) {
	// L-REN-04
	ctx := context.Background()
	v, x, dir := corpusVault(t, "", map[string]string{
		"B/Dup.md":   "b\n",
		"A/Thing.md": "[[../Src]] [[Dup]]\n",
		"Uses.md":    "[[Dup]] [[Thing]]\n",
		"A/Rel2.md":  "[[../Src]]\n",
		"Escaped.md": "---\nrel: \"[[Tar\\u0067et]]\"\n---\n",
	})
	// Moving A/Thing.md to C/Dup.md makes [[Dup]] ambiguous between B/Dup and
	// C/Dup: links to B/Dup get B/Dup's full path, and [[Thing]] follows.
	plan, err := x.PlanMoves(ctx, map[string]string{"A/Thing.md": "C/Dup.md"})
	applyPlan(t, v, plan, err)
	if got := read(t, dir, "Uses.md"); got != "[[B/Dup]] [[C/Dup]]\n" {
		t.Errorf("L-REN-04 Uses.md = %q", got)
	}
	// In the moved note, [[Dup]] would now find the note itself, so it gets
	// B/Dup's full path; [[../Src]] still works from C/ and stays.
	if got := read(t, dir, "C/Dup.md"); got != "[[../Src]] [[B/Dup]]\n" {
		t.Errorf("links in a moved note = %q", got)
	}
	// A relative link that breaks when its note moves deeper is fixed.
	plan, err = x.PlanMoves(ctx, map[string]string{"A/Rel2.md": "A/X/Rel2.md"})
	applyPlan(t, v, plan, err)
	if got := read(t, dir, "A/X/Rel2.md"); got != "[[Src]]\n" {
		t.Errorf("relative link in a moved note = %q", got)
	}
	// A frontmatter link written with YAML escapes can't be patched: it is
	// reported and left alone.
	plan, err = x.PlanMoves(ctx, map[string]string{"Notes/Target.md": "Notes/T2.md"})
	if err != nil {
		t.Fatal(err)
	}
	var skipped bool
	for _, s := range plan.Skipped {
		skipped = skipped || (s.Source == "Escaped.md" && strings.Contains(s.Reason, "escapes"))
	}
	if !skipped {
		t.Errorf("L-REN-04 skipped = %+v", plan.Skipped)
	}
	for _, c := range plan.Changes {
		if c.Path == "Escaped.md" {
			t.Errorf("L-REN-04 escaped note changed")
		}
	}
}

func TestHeadingRename(t *testing.T) {
	// L-REN-08
	ctx := context.Background()
	v, x, dir := corpusVault(t, "", map[string]string{"Hdr.md": "## Closed ##\n\n[[Hdr#Closed]] [[Target#Sub]]\n"})
	before := sums(t, dir)
	f, _ := v.Read(ctx, "Notes/Target.md")
	plan, err := x.PlanHeadingRename(ctx, "Notes/Target.md", f.ETag, "Top#Sub", "New Sub")
	applyPlan(t, v, plan, err)
	if got := read(t, dir, "Notes/Target.md"); got != "# Top\n\n## New Sub\n\npara ^b1\n\nSee [[#New Sub]] and [[#Top#New Sub|here]].\n" {
		t.Errorf("L-REN-08 note = %q", got)
	}
	if got := read(t, dir, "Src.md"); !strings.Contains(got, "[y](<Target.md#New%20Sub>)") || !strings.Contains(got, "[[Target#Top#New Sub]]") || !strings.Contains(got, "[[Target#Top|t]]") {
		t.Errorf("L-REN-08 Src.md = %s", got)
	}
	if got := read(t, dir, "Map.canvas"); !strings.Contains(got, `"subpath":"#New Sub"`) {
		t.Errorf("L-REN-08 canvas = %s", got)
	}
	f, _ = v.Read(ctx, "Notes/Target.md")
	plan, err = x.PlanHeadingRename(ctx, "Notes/Target.md", f.ETag, "New Sub", "Sub")
	applyPlan(t, v, plan, err)
	src := read(t, dir, "Src.md")
	if !strings.Contains(src, "[y](<Target.md#Sub>)") {
		t.Errorf("round trip Src.md = %s", src)
	}
	// The %20 form differs from the original <…#Sub> only if the heading has
	// a space; this one doesn't, so the round trip is exact.
	if after := sums(t, dir); !maps.Equal(before, after) {
		t.Errorf("L-REN-07 heading round trip changed the vault")
	}
	// Closing #s stay.
	h, _ := v.Read(ctx, "Hdr.md")
	plan, err = x.PlanHeadingRename(ctx, "Hdr.md", h.ETag, "Closed", "Open")
	applyPlan(t, v, plan, err)
	if got := read(t, dir, "Hdr.md"); got != "## Open ##\n\n[[Hdr#Open]] [[Target#Sub]]\n" {
		t.Errorf("L-REN-08 closing #s = %q", got)
	}
	for _, bad := range []string{"", "a#b", "a|b", "[[x]]", "^x"} {
		if _, err := x.PlanHeadingRename(ctx, "Hdr.md", h.ETag, "Open", bad); err == nil {
			t.Errorf("L-REN-08 new heading %q accepted", bad)
		}
	}
	if _, err := x.PlanHeadingRename(ctx, "Hdr.md", "stale", "Open", "X"); err == nil {
		t.Error("stale etag accepted")
	}
}

func TestBlockRename(t *testing.T) {
	// L-REN-09
	ctx := context.Background()
	v, x, dir := corpusVault(t, "", nil)
	f, _ := v.Read(ctx, "Notes/Target.md")
	plan, err := x.PlanBlockRename(ctx, "Notes/Target.md", f.ETag, "^B1", "new-id")
	applyPlan(t, v, plan, err)
	if got := read(t, dir, "Notes/Target.md"); !strings.Contains(got, "para ^new-id\n") {
		t.Errorf("L-REN-09 note = %q", got)
	}
	if got := read(t, dir, "Src.md"); !strings.Contains(got, "![[Target#^new-id]]") {
		t.Errorf("L-REN-09 Src.md = %s", got)
	}
	f, _ = v.Read(ctx, "Notes/Target.md")
	for _, bad := range []string{"bad_id", "", "a b"} {
		if _, err := x.PlanBlockRename(ctx, "Notes/Target.md", f.ETag, "new-id", bad); err == nil {
			t.Errorf("L-REN-09 new ID %q accepted", bad)
		}
	}
	if _, err := x.PlanBlockRename(ctx, "Notes/Target.md", f.ETag, "missing", "x1"); err == nil {
		t.Error("L-REN-09 missing block accepted")
	}
}

func TestRelPathAndTargets(t *testing.T) {
	// L-REN-02
	for _, tt := range []struct{ dir, to, want string }{
		{"", "a/b.md", "a/b.md"},
		{"a", "a/b.md", "b.md"},
		{"a/c", "a/b.md", "../b.md"},
		{"x/y", "a/b.md", "../../a/b.md"},
	} {
		if got := relPath(tt.dir, tt.to); got != tt.want {
			t.Errorf("relPath(%q, %q) = %q", tt.dir, tt.to, got)
		}
	}
	n := newNames([]string{"a/My Note.md", "b/Dup.md", "c/Dup.md", "img.png"})
	for _, tt := range []struct {
		to, format string
		style      Style
		want       string
	}{
		{"a/My Note.md", "shortest", Wikilink, "My Note"},
		{"a/My Note.md", "shortest", Markdown, "My%20Note.md"},
		{"b/Dup.md", "shortest", Wikilink, "b/Dup"},
		{"img.png", "shortest", Wikilink, "img.png"},
		{"a/My Note.md", "relative", Wikilink, "../a/My Note"},
		{"a/My Note.md", "absolute", Canvas, "a/My Note.md"},
	} {
		if got := n.linkTarget("z/src.md", tt.to, tt.format, tt.style); got != tt.want {
			t.Errorf("linkTarget(%q, %s, %d) = %q; want %q", tt.to, tt.format, tt.style, got, tt.want)
		}
	}
}
