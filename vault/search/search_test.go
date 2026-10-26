package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
	"github.com/swiftj/obsidian-mcp/vault"
)

func write(t testing.TB, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openIndex(t testing.TB, dir string) *Index {
	t.Helper()
	v, err := vault.Open(context.Background(), dir, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return New(v, Options{})
}

// fixture is a made-up vault for the search tests.
func fixture(t *testing.T) (string, *Index) {
	t.Helper()
	dir := testutil.CopyVault(t, "basic")
	write(t, dir, ".obsidian/app.json", `{"userIgnoreFilters":["Archive/"]}`)
	write(t, dir, "Garden/Tomatoes.md", "---\nstatus: Draft\ntags:\n  - Plants/Veg\nrating: 4\n---\n# Tomatoes\n\nTomatoes need sun.\nWater the tomatoes daily.\nA red fruit.\n")
	write(t, dir, "Garden/Basil.md", "---\nstatus: done\naliases: [Herb]\n---\nBasil likes sun and tomatoes. #plants/herb #Inbox\n")
	write(t, dir, "Kitchen/Sauce.md", "Tomato sauce uses basil. #inbox/to-read\n")
	write(t, dir, "Archive/Old tomatoes.md", "Tomatoes tomatoes tomatoes. #archived\n")
	write(t, dir, "日本.md", "東京の庭\n")
	return dir, openIndex(t, dir)
}

func paths(p *Page) []string {
	var out []string
	for _, r := range p.Results {
		out = append(out, r.Path)
	}
	return out
}

func mustSearch(t *testing.T, x *Index, q Query) *Page {
	t.Helper()
	p, err := x.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search %+v: %v", q, err)
	}
	return p
}

func TestSearchRanking(t *testing.T) {
	// V-SRCH-01
	_, x := fixture(t)
	p := mustSearch(t, x, Query{Text: "tomatoes"})
	if got := paths(p); !slices.Equal(got, []string{"Garden/Tomatoes.md", "Garden/Basil.md"}) {
		t.Fatalf("V-SRCH-01 ranking = %q", got)
	}
	top := p.Results[0]
	if top.Score <= p.Results[1].Score {
		t.Errorf("V-SRCH-01 scores %v, %v", top.Score, p.Results[1].Score)
	}
	want := []Snippet{{7, "# Tomatoes"}, {9, "Tomatoes need sun."}, {10, "Water the tomatoes daily."}}
	if !slices.Equal(top.Snippets, want) {
		t.Errorf("V-SRCH-01 snippets = %+v", top.Snippets)
	}
	// Every word must match; case is ignored.
	if got := paths(mustSearch(t, x, Query{Text: "SUN basil"})); !slices.Equal(got, []string{"Garden/Basil.md"}) {
		t.Errorf("V-SRCH-01 AND = %q", got)
	}
	// A phrase must appear as written.
	if got := paths(mustSearch(t, x, Query{Text: `"water the tomatoes"`})); !slices.Equal(got, []string{"Garden/Tomatoes.md"}) {
		t.Errorf("V-SRCH-01 phrase = %q", got)
	}
	if got := paths(mustSearch(t, x, Query{Text: `"tomatoes water"`})); len(got) != 0 {
		t.Errorf("V-SRCH-01 phrase out of order = %q", got)
	}
	// Each Han character is a token.
	if got := paths(mustSearch(t, x, Query{Text: "東京"})); !slices.Equal(got, []string{"日本.md"}) {
		t.Errorf("V-SRCH-01 CJK = %q", got)
	}
	if got := paths(mustSearch(t, x, Query{Text: "zzz"})); len(got) != 0 {
		t.Errorf("V-SRCH-01 no match = %q", got)
	}
}

func TestSearchPaging(t *testing.T) {
	// V-SRCH-01
	_, x := fixture(t)
	var all []string
	q := Query{Text: "tomatoes", Limit: 1}
	for range 5 {
		p := mustSearch(t, x, q)
		if p.Total != 2 {
			t.Fatalf("V-SRCH-01 total = %d", p.Total)
		}
		all = append(all, paths(p)...)
		if p.Next == "" {
			break
		}
		q.Cursor = p.Next
	}
	if !slices.Equal(all, []string{"Garden/Tomatoes.md", "Garden/Basil.md"}) {
		t.Errorf("V-SRCH-01 pages = %q", all)
	}
	if _, err := x.Search(context.Background(), Query{Cursor: "x"}); !errors.Is(err, ErrBadCursor) {
		t.Errorf("bad cursor: %v", err)
	}
	if p := mustSearch(t, x, Query{Text: "tomatoes", Cursor: "9"}); len(p.Results) != 0 || p.Next != "" {
		t.Errorf("cursor past the end = %+v", p)
	}
}

func TestSearchFilters(t *testing.T) {
	// V-SRCH-02, V-SRCH-04
	dir, x := fixture(t)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "Kitchen", "Sauce.md"), old, old); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		q    Query
		want []string
	}{
		{"nested tag", Query{Tags: []string{"plants"}}, []string{"Garden/Basil.md", "Garden/Tomatoes.md"}},
		{"tag case and #", Query{Tags: []string{"#PLANTS/veg"}}, []string{"Garden/Tomatoes.md"}},
		{"parent tag", Query{Tags: []string{"inbox"}}, []string{"Garden/Basil.md", "Kitchen/Sauce.md"}},
		{"tags AND", Query{Tags: []string{"inbox", "plants"}}, []string{"Garden/Basil.md"}},
		{"no partial tag", Query{Tags: []string{"plant"}}, nil},
		{"property value", Query{Props: map[string]string{"status": "draft"}}, []string{"Garden/Tomatoes.md"}},
		{"property number", Query{Props: map[string]string{"rating": "4"}}, []string{"Garden/Tomatoes.md"}},
		{"property list item", Query{Props: map[string]string{"aliases": "herb"}}, []string{"Garden/Basil.md"}},
		{"deprecated key", Query{Props: map[string]string{"alias": "herb"}}, []string{"Garden/Basil.md"}},
		{"property present", Query{Props: map[string]string{"status": ""}}, []string{"Garden/Basil.md", "Garden/Tomatoes.md"}},
		{"path glob", Query{Path: "garden/**"}, []string{"Garden/Basil.md", "Garden/Tomatoes.md"}},
		{"since", Query{Text: "basil", Since: time.Now().Add(-time.Hour)}, []string{"Garden/Basil.md"}},
		{"text and filters", Query{Text: "sun", Tags: []string{"plants"}, Props: map[string]string{"status": "done"}, Path: "Garden/*"}, []string{"Garden/Basil.md"}},
	} {
		if got := paths(mustSearch(t, x, tt.q)); !slices.Equal(got, tt.want) {
			t.Errorf("V-SRCH-02 %s = %q; want %q", tt.name, got, tt.want)
		}
	}
	if _, err := x.Search(context.Background(), Query{Path: "["}); err == nil {
		t.Error("V-SRCH-02 bad glob accepted")
	}
}

func TestExcludedAndTags(t *testing.T) {
	// V-SRCH-03, V-SRCH-05
	dir, x := fixture(t)
	write(t, dir, "Later.md", "#INBOX/Later and #inbox\n")
	if got := paths(mustSearch(t, x, Query{Text: "tomatoes"})); slices.Contains(got, "Archive/Old tomatoes.md") {
		t.Errorf("V-SRCH-03 excluded note searched: %q", got)
	}
	tags, err := x.Tags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tg := range tags {
		got = append(got, fmt.Sprintf("%s:%d:%s", tg.Name, tg.Count, tg.Parent))
	}
	// Basil.md (#Inbox) comes before Later.md in path order.
	want := []string{"Inbox:3:", "INBOX/Later:1:Inbox", "inbox/to-read:1:Inbox", "plants:2:", "plants/herb:1:plants", "Plants/Veg:1:plants"}
	if !slices.Equal(got, want) {
		t.Errorf("V-SRCH-05 tags = %q; want %q", got, want)
	}
}

func TestIncremental(t *testing.T) {
	// V-SRCH-06
	dir, x := fixture(t)
	mustSearch(t, x, Query{Text: "tomatoes"})
	reads := x.sc.Reads
	write(t, dir, "Kitchen/Sauce.md", "Now with extra tomatoes.\n")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "Kitchen", "Sauce.md"), future, future); err != nil {
		t.Fatal(err)
	}
	if got := paths(mustSearch(t, x, Query{Text: "tomatoes"})); !slices.Contains(got, "Kitchen/Sauce.md") {
		t.Errorf("V-SRCH-06 edit not seen: %q", got)
	}
	if x.sc.Reads != reads+1 {
		t.Errorf("V-SRCH-06 refresh read %d files; want 1", x.sc.Reads-reads)
	}
	if err := os.Remove(filepath.Join(dir, "Garden", "Basil.md")); err != nil {
		t.Fatal(err)
	}
	if got := paths(mustSearch(t, x, Query{Text: "basil"})); len(got) != 0 {
		t.Errorf("V-SRCH-06 delete not seen: %q", got)
	}
	if got := paths(mustSearch(t, x, Query{Tags: []string{"plants/herb"}})); len(got) != 0 {
		t.Errorf("V-SRCH-06 deleted note's tags remain: %q", got)
	}
}

func TestLargeVault(t *testing.T) {
	// V-SRCH-06: 10k made-up notes index inside the call timeout.
	if testing.Short() {
		t.Skip("large vault")
	}
	dir := t.TempDir()
	words := strings.Fields("alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau upsilon")
	for i := range 10000 {
		var sb strings.Builder
		fmt.Fprintf(&sb, "---\nn: %d\ntags: [t%d]\n---\n# Note %d\n\n", i, i%50, i)
		for j := range 200 {
			sb.WriteString(words[(i*7+j*13)%len(words)])
			sb.WriteString(" ")
			if j%20 == 19 {
				fmt.Fprintf(&sb, "[[Note %d]]\n", (i+j)%10000)
			}
		}
		write(t, dir, fmt.Sprintf("F%02d/Note %d.md", i%100, i), sb.String())
	}
	x := openIndex(t, dir)
	start := time.Now()
	p := mustSearch(t, x, Query{Text: "alpha omicron", Tags: []string{"t7"}})
	took := time.Since(start)
	if p.Total == 0 {
		t.Error("V-SRCH-06 no results")
	}
	if took > 30*time.Second {
		t.Errorf("V-SRCH-06 first build took %v", took)
	}
	start = time.Now()
	mustSearch(t, x, Query{Text: "beta"})
	t.Logf("10k notes: build+query %v, warm query %v", took, time.Since(start))
}

func TestConcurrentQueries(t *testing.T) {
	// MCP tool calls run concurrently; run under -race.
	_, x := fixture(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := x.Search(context.Background(), Query{Text: "tomatoes"}); err != nil {
				t.Error(err)
			}
			if _, err := x.Tags(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}
