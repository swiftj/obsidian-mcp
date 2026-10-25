package ofm

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
)

// These tests replay the Phase 0 probe vault and expect exactly what
// Obsidian 1.14.4 reported for it (docs/DECISIONS.md D-006).

func probeDoc(t *testing.T, rel string) *Doc {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testutil.RepoRoot(), "testdata", "probes", "vault", rel))
	if err != nil {
		t.Fatal(err)
	}
	return Parse(b)
}

func TestProbeFrontmatter(t *testing.T) {
	// O-FM-01, O-FM-02
	tests := map[string]bool{
		"plain.md": true, "crlf.md": true, "bom.md": true, "json.md": true, "empty.md": true, "invalid-yaml.md": true,
		"trailing-space.md": false, "leading-blank.md": false, "dots-close.md": false,
		"no-close.md": false, "four-dash.md": false, "indented.md": false,
	}
	for name, want := range tests {
		d := probeDoc(t, "pr12/"+name)
		if got := d.Frontmatter != nil; got != want {
			t.Errorf("O-FM-01 %s: frontmatter = %v; want %v", name, got, want)
		}
	}
	d := probeDoc(t, "pr12/json.md")
	if !d.Frontmatter.JSON {
		t.Error("O-FM-02 JSON frontmatter not recognised")
	}
	if d := probeDoc(t, "pr12/bom.md"); !d.BOM || d.Frontmatter.Start != 3 {
		t.Errorf("O-FM-01 BOM frontmatter starts at %d", d.Frontmatter.Start)
	}
	if d := probeDoc(t, "pr12/plain.md"); string(d.Src[d.Frontmatter.Body.Start:d.Frontmatter.Body.End]) != "key: value\n" {
		t.Errorf("O-FM-01 body = %q", d.Src[d.Frontmatter.Body.Start:d.Frontmatter.Body.End])
	}
	if d := probeDoc(t, "pr12/crlf.md"); string(d.Src[d.BodyStart():]) != "body\r\n" {
		t.Errorf("O-FM-01 CRLF body start wrong: %q", d.Src[d.BodyStart():])
	}
}

func TestProbeHeadings(t *testing.T) {
	// O-HEAD-01
	d := probeDoc(t, "pr13/Heads.md")
	type h struct {
		text  string
		level int
	}
	want := []h{
		{"Simple", 1}, {"Colon: here", 2}, {"Hash # sign", 2}, {"Pipe | bar", 2}, {"Caret ^ char", 2},
		{"With [[pr13/Other|link]] inside", 2}, {"**Bold** and *ital*", 2}, {"Trailing hashes", 2},
		{"Percent %% x", 2}, {"Dup", 2}, {"Dup", 2}, {"Setext One", 1}, {"Setext Two", 2}, {"Indented three", 3},
	}
	var got []h
	for _, x := range d.Headings {
		got = append(got, h{x.Text, x.Level})
	}
	if !slices.Equal(got, want) {
		t.Errorf("O-HEAD-01 headings =\n%v\nwant\n%v", got, want)
	}
	if len(d.Links) != 1 || d.Links[0].Target != "pr13/Other" || d.Links[0].Display != "link" {
		t.Errorf("link in heading = %+v", d.Links)
	}
	var tags []string
	for _, tg := range d.Tags {
		tags = append(tags, tg.Name)
	}
	if !slices.Equal(tags, []string{"NoSpace"}) {
		t.Errorf("O-HEAD-01 #NoSpace should be a tag, not a heading: tags %v", tags)
	}
	// Sections: "Simple" (H1) runs to "Setext One" (H1).
	s0 := d.Sections[0]
	if s0.Start != 0 || s0.End != d.Headings[11].Start {
		t.Errorf("O-HEAD-01 H1 section = %v; want to %d", s0.Span, d.Headings[11].Start)
	}
	// The setext H2 contains the H3 after it and runs to the end.
	if s := d.Sections[12]; s.End != len(d.Src) || d.Sections[13].End != len(d.Src) {
		t.Errorf("O-HEAD-01 setext H2 section ends at %d", s.End)
	}
}

func TestProbeBlockIDs(t *testing.T) {
	// O-BLK-01
	d := probeDoc(t, "pr13/Blocks.md")
	got := map[string]string{}
	for _, b := range d.BlockIDs {
		got[b.ID] = string(d.Src[b.Block.Start:b.Block.End])
	}
	want := map[string]string{
		"para-1":      "Paragraph with id. ^para-1",
		"li-1":        "item one ^li-1",
		"after-quote": "> quoted",
		"UPPER-2":     "Upper id. ^UPPER-2",
		"after-table": "| a | b |\n| - | - |\n| 1 | 2 |",
	}
	if len(got) != len(want) {
		t.Errorf("O-BLK-01 ids = %v", got)
	}
	for id, text := range want {
		if got[id] != text {
			t.Errorf("O-BLK-01 ^%s labels %q; want %q", id, got[id], text)
		}
	}
}

func TestProbeLinkContexts(t *testing.T) {
	// O-HTML-01, O-CMT-01, O-CODE-01, O-CODE-02, O-MATH-01, O-LINK-01, O-LINK-04
	counted := map[string]func(Link) bool{
		"normal":         func(l Link) bool { return !l.InComment },
		"inline-comment": func(l Link) bool { return l.InComment },
		"block-comment":  func(l Link) bool { return l.InComment },
		"html-inline":    func(Link) bool { return true },
		"callout":        func(Link) bool { return true },
		"table":          func(l Link) bool { return l.Target == "Target14" && l.Display == "x" },
		"embed":          func(l Link) bool { return l.Embed },
		"mdlink":         func(l Link) bool { return l.Markdown && l.Target == "Target14.md" },
	}
	for name, check := range counted {
		d := probeDoc(t, "pr14/in-"+name+".md")
		if len(d.Links) != 1 || !check(d.Links[0]) {
			t.Errorf("pr14 %s: links = %+v", name, d.Links)
		}
	}
	for _, name := range []string{"code-fence", "inline-code", "math-inline", "math-block", "html", "escaped", "indented-code"} {
		if d := probeDoc(t, "pr14/in-"+name+".md"); len(d.Links) != 0 {
			t.Errorf("pr14 %s: links = %+v; want none", name, d.Links)
		}
	}
}

func TestProbeMarkdownLinks(t *testing.T) {
	// O-LINK-02: [a]–[g] are links, [h] (unencoded space) is not.
	d := probeDoc(t, "pr10/sub/src.md")
	var got []string
	for _, l := range d.Links {
		got = append(got, l.Display+"="+l.Target)
	}
	want := []string{"a=../Md Target.md", "b=../Md Target.md", "c=Md Target.md", "d=pr10/Md Target.md",
		"e=./../Md Target.md", "f=../Md Target", "g=/pr10/Md Target.md"}
	if !slices.Equal(got, want) {
		t.Errorf("O-LINK-02 links =\n%q\nwant\n%q", got, want)
	}
}

func TestProbeTags(t *testing.T) {
	// O-TAG-01
	d := probeDoc(t, "pr15/Tags.md")
	var got []string
	for _, tg := range d.Tags {
		got = append(got, tg.Name)
	}
	want := []string{"plain", "trail", "with-dash_und/nest", "y1984", "émoji🙂", "tag", "bold", "after", "a", "b",
		"日本語", "UPPER", "upper", "-lead", "_lead", "/slash", "end/"}
	if !slices.Equal(got, want) {
		t.Errorf("O-TAG-01 tags =\n%q\nwant\n%q", got, want)
	}
	if NormTag("#End/") != "end" {
		t.Error("O-TAG-01 NormTag")
	}
}
