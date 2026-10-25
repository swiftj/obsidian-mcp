package ofm

import (
	"fmt"
	"slices"
	"testing"
)

func TestWikilinkForms(t *testing.T) {
	// O-LINK-01, O-LINK-03, O-LINK-04, O-LINK-05
	tests := []struct {
		src                      string
		target, subpath, display string
		hasDisplay, embed        bool
		w, h                     int
	}{
		{"[[Note]]", "Note", "", "", false, false, 0, 0},
		{"[[Note.md]]", "Note.md", "", "", false, false, 0, 0},
		{"[[Folder/Note#Heading]]", "Folder/Note", "#Heading", "", false, false, 0, 0},
		{"[[Note#H#Sub]]", "Note", "#H#Sub", "", false, false, 0, 0},
		{"[[Note#^block-1]]", "Note", "#^block-1", "", false, false, 0, 0},
		{"[[#Local heading]]", "", "#Local heading", "", false, false, 0, 0},
		{"[[Note|shown text]]", "Note", "", "shown text", true, false, 0, 0},
		{"[[Note#H|shown]]", "Note", "#H", "shown", true, false, 0, 0},
		{"![[Note]]", "Note", "", "", false, true, 0, 0},
		{"![[img.png|100]]", "img.png", "", "", false, true, 100, 0},
		{"![[pic.JPG|100x145]]", "pic.JPG", "", "", false, true, 100, 145},
		{"![[Note|100]]", "Note", "", "100", true, true, 0, 0},
		{"![[doc.pdf#page=3]]", "doc.pdf", "#page=3", "", false, true, 0, 0},
		{"![[doc.pdf#page=3&height=400]]", "doc.pdf", "#page=3&height=400", "", false, true, 0, 0},
		{"[[Board.canvas]]", "Board.canvas", "", "", false, false, 0, 0},
		{"![[Tasks.base#View]]", "Tasks.base", "#View", "", false, true, 0, 0},
	}
	for _, tt := range tests {
		d := Parse([]byte("x " + tt.src + " y\n"))
		if len(d.Links) != 1 {
			t.Errorf("O-LINK-01 %q: %d links", tt.src, len(d.Links))
			continue
		}
		l := d.Links[0]
		if l.Target != tt.target || l.Subpath != tt.subpath || l.Display != tt.display || l.HasDisplay != tt.hasDisplay ||
			l.Embed != tt.embed || l.Width != tt.w || l.Height != tt.h || l.Markdown {
			t.Errorf("O-LINK-01 %q = %+v", tt.src, l)
		}
		if got := string(d.Src[l.Start:l.End]); got != tt.src {
			t.Errorf("O-LINK-01 %q span = %q", tt.src, got)
		}
		raw := string(d.Src[l.TargetSpan.Start:l.TargetSpan.End]) + string(d.Src[l.SubpathSpan.Start:l.SubpathSpan.End])
		if raw != tt.target+tt.subpath {
			t.Errorf("O-LINK-03 %q raw target+subpath = %q", tt.src, raw)
		}
	}
	for _, notLink := range []string{"[[]]", "[[ ]]", "[[unclosed", "\\[[Escaped]]", "`[[code]]`", "[Note]"} {
		if d := Parse([]byte(notLink + "\n")); len(d.Links) != 0 {
			t.Errorf("O-LINK-01 %q gave links %+v", notLink, d.Links)
		}
	}
	// A table cell's "\|" separates display text.
	d := Parse([]byte("| a | b |\n| - | - |\n| [[T\\|shown]] | 2 |\n"))
	if len(d.Links) != 1 || d.Links[0].Target != "T" || d.Links[0].Display != "shown" {
		t.Errorf("O-LINK-01 table link = %+v", d.Links)
	}
}

func TestMarkdownLinkForms(t *testing.T) {
	// O-LINK-02
	tests := []struct {
		src, target, subpath string
		external, embed      bool
	}{
		{"[t](Note.md)", "Note.md", "", false, false},
		{"[t](Note)", "Note", "", false, false},
		{"[t](My%20Note.md#Some%20Head)", "My Note.md", "#Some Head", false, false},
		{"[t](<My Note.md>)", "My Note.md", "", false, false},
		{`[t](Note.md "title")`, "Note.md", "", false, false},
		{"[t](https://example.com/a#b)", "https://example.com/a", "#b", true, false},
		{"[t](mailto:a@example.com)", "mailto:a@example.com", "", true, false},
		{"![alt](img.png)", "img.png", "", false, true},
		{"[t](a_(b).md)", "a_(b).md", "", false, false},
	}
	for _, tt := range tests {
		d := Parse([]byte(tt.src + "\n"))
		if len(d.Links) != 1 {
			t.Errorf("O-LINK-02 %q: links %+v", tt.src, d.Links)
			continue
		}
		l := d.Links[0]
		if !l.Markdown || l.Target != tt.target || l.Subpath != tt.subpath || l.External != tt.external || l.Embed != tt.embed {
			t.Errorf("O-LINK-02 %q = %+v", tt.src, l)
		}
	}
	for _, notLink := range []string{"[t](My Note.md)", "[t]()", "[t] (x.md)", "[t](x.md"} {
		if d := Parse([]byte(notLink + "\n")); len(d.Links) != 0 {
			t.Errorf("O-LINK-02 %q gave links %+v", notLink, d.Links)
		}
	}
}

func TestCodeAndMath(t *testing.T) {
	// O-CODE-01, O-CODE-02, O-MATH-01
	src := "```js\n[[a]] #t\n```\n\n~~~~\n```\n[[b]]\n~~~~\n\n    [[c]]\n\nUse `` x ` [[d]] `` and $[[e]]$ and $$[[f]]$$ and `[[g]]`.\n\n$$\n[[h]]\n$$\n\nCost $5 and $6 [[ok]].\n"
	d := Parse([]byte(src))
	if len(d.Links) != 1 || d.Links[0].Target != "ok" {
		t.Errorf("O-CODE-01 links = %+v", d.Links)
	}
	if len(d.Tags) != 0 {
		t.Errorf("O-CODE-01 tags = %+v", d.Tags)
	}
	var kinds []CodeKind
	for _, c := range d.Code {
		kinds = append(kinds, c.Kind)
	}
	if !slices.Equal(kinds, []CodeKind{CodeFenced, CodeFenced, CodeIndented, CodeInline, CodeInline}) || d.Code[0].Info != "js" {
		t.Errorf("O-CODE-02 code = %+v", d.Code)
	}
	if len(d.Math) != 3 {
		t.Errorf("O-MATH-01 math = %v", d.Math)
	}
	// An unclosed fence runs to the end.
	if d := Parse([]byte("```\n[[x]]\n")); len(d.Links) != 0 || len(d.Code) != 1 {
		t.Errorf("O-CODE-01 unclosed fence: %+v", d)
	}
}

func TestComments(t *testing.T) {
	// O-CMT-01: comment links count; tags in comments don't (D-009).
	d := Parse([]byte("a %%[[x]] #t%% b\n\n%%\n[[y]]\n#u\n%%\n\nnot %% closed [[z]]\n\n%%\n[[w]] unclosed\n"))
	var got []string
	for _, l := range d.Links {
		got = append(got, fmt.Sprintf("%s:%v", l.Target, l.InComment))
	}
	if !slices.Equal(got, []string{"x:true", "y:true", "z:false", "w:true"}) {
		t.Errorf("O-CMT-01 links = %v", got)
	}
	if len(d.Tags) != 0 {
		t.Errorf("O-CMT-01 tags in comments = %+v", d.Tags)
	}
	if len(d.Comments) != 3 {
		t.Errorf("O-CMT-01 comments = %v", d.Comments)
	}
}

func TestCallouts(t *testing.T) {
	// O-CALL-01
	src := "> [!TIP]- Fold me\n> body [[a]]\n> > [!faq]+\n> > inner\n> > > [!custom] x\n\n> plain quote\n\n> [!note]\n"
	d := Parse([]byte(src))
	var got []string
	for _, c := range d.Callouts {
		fold := c.Fold
		if fold == 0 {
			fold = '0'
		}
		got = append(got, fmt.Sprintf("%s/%s/%c/%s/%d", c.Type, c.Canon, fold, c.Title, c.Depth))
	}
	want := []string{"tip/tip/-/Fold me/1", "faq/question/+//2", "custom/note/0/x/3", "note/note/0//1"}
	if !slices.Equal(got, want) {
		t.Errorf("O-CALL-01 callouts =\n%q\nwant\n%q", got, want)
	}
	for alias, canon := range map[string]string{"summary": "abstract", "tldr": "abstract", "hint": "tip", "important": "tip",
		"check": "success", "done": "success", "help": "question", "caution": "warning", "attention": "warning",
		"fail": "failure", "missing": "failure", "error": "danger", "cite": "quote", "bug": "bug", "example": "example", "info": "info", "todo": "todo"} {
		if c, ok := parseCallout([]byte("[!" + alias + "]")); !ok || c.Canon != canon {
			t.Errorf("O-CALL-01 %s → %s", alias, c.Canon)
		}
	}
}

func TestTasks(t *testing.T) {
	// O-TASK-01
	d := Parse([]byte("- [ ] open\n- [x] done\n- [-] cancelled\n- [?] q\n  - [>] nested\n1. [/] ordered\n- [] not\n- [xx] not\n- plain\n"))
	var got []string
	for _, tk := range d.Tasks {
		got = append(got, string(tk.Status))
	}
	if !slices.Equal(got, []string{" ", "x", "-", "?", ">", "/"}) {
		t.Errorf("O-TASK-01 statuses = %q", got)
	}
}

func TestInlineSpans(t *testing.T) {
	// O-INL-01: highlights and footnotes are recognised; their "^" is never a block ID.
	d := Parse([]byte("Some ==highlight [[a]]== text[^1] and ^[inline [[b]] note]\n\nEnds with footnote [^note]\n\nEnds ^[inline]\n"))
	if len(d.Highlights) != 1 || len(d.Footnotes) != 4 {
		t.Errorf("O-INL-01 highlights %v footnotes %v", d.Highlights, d.Footnotes)
	}
	if len(d.BlockIDs) != 0 {
		t.Errorf("O-INL-01 block IDs = %+v", d.BlockIDs)
	}
	if len(d.Links) != 2 {
		t.Errorf("O-INL-01 links = %+v", d.Links)
	}
}

func TestBlockIDRules(t *testing.T) {
	// O-BLK-01
	src := "- a\n- b\n\n^list-id\n\npara\n\n^not-after-para\n\n## H ^no\n\n> [!note]\n> c\n\n^callout-id\n"
	d := Parse([]byte(src))
	var got []string
	for _, b := range d.BlockIDs {
		got = append(got, b.ID)
	}
	if !slices.Equal(got, []string{"list-id", "callout-id"}) {
		t.Errorf("O-BLK-01 ids = %v", got)
	}
}

// FuzzParse checks O-SPAN-01: Parse never panics, spans lie inside the
// source, and any two spans either nest or don't overlap.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"---\na: 1\n---\n# H\ntext [[l|d]] #tag ^id\n",
		"> [!note]\n> - [ ] task [[x]]\n> ```\n> [[y]]\n",
		"a ==b `c` [[d]]== %%[[e]]%% $f$ [^1] ^[g [[h]]]\n",
		"| a |\n| - |\n| [[x\\|y]] |\n\n^tid\n",
		"Setext\n===\n\n- a\n  - b\n\n    code\n<div>\n[[z]]\n</div>\n",
		"\xef\xbb\xbf---\r\n{\"a\":1}\r\n---\r\n%%\r\n[[c]]\r\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		d := Parse(src)
		spans := allSpans(d)
		for i, a := range spans {
			if a.Start < 0 || a.End > len(src) || a.Start > a.End {
				t.Fatalf("O-SPAN-01 span %v outside source of %d bytes", a, len(src))
			}
			for _, b := range spans[i+1:] {
				overlap := a.Start < b.End && b.Start < a.End
				nested := (a.Start <= b.Start && b.End <= a.End) || (b.Start <= a.Start && a.End <= b.End)
				if overlap && !nested {
					t.Fatalf("O-SPAN-01 spans %v %q and %v %q cross in %q", a, src[a.Start:a.End], b, src[b.Start:b.End], src)
				}
			}
		}
	})
}

func allSpans(d *Doc) []Span {
	var out []Span
	if d.Frontmatter != nil {
		out = append(out, d.Frontmatter.Span, d.Frontmatter.Body)
	}
	var walk func([]Block)
	walk = func(bs []Block) {
		for _, b := range bs {
			out = append(out, b.Span)
			walk(b.Children)
		}
	}
	walk(d.Blocks)
	for _, x := range d.Headings {
		out = append(out, x.Span)
	}
	for _, x := range d.Sections {
		out = append(out, x.Span)
	}
	for _, x := range d.BlockIDs {
		out = append(out, x.Span, x.Block)
	}
	for _, x := range d.Links {
		out = append(out, x.Span, x.TargetSpan, x.SubpathSpan, x.DisplaySpan)
	}
	for _, x := range d.Tags {
		out = append(out, x.Span)
	}
	for _, x := range d.Callouts {
		out = append(out, x.Span)
	}
	for _, x := range d.Tasks {
		out = append(out, x.Span, x.Box)
	}
	for _, x := range d.Code {
		out = append(out, x.Span)
	}
	out = append(out, d.Comments...)
	out = append(out, d.Math...)
	out = append(out, d.Highlights...)
	out = append(out, d.Footnotes...)
	return out
}

func TestSpanString(t *testing.T) {
	if BlockList.String() != "list" || BlockKind(99).String() != "unknown" || (Span{2, 5}).Len() != 3 {
		t.Error("String/Len helpers")
	}
}
