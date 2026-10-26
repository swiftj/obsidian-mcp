package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/swiftj/obsidian-mcp/vault"
	"github.com/swiftj/obsidian-mcp/vault/links"
	"github.com/swiftj/obsidian-mcp/vault/ofm"
	"github.com/swiftj/obsidian-mcp/vault/props"
	"github.com/swiftj/obsidian-mcp/vault/search"
)

func (s *Server) addReadTools() {
	addTool(s, "list_vaults", "List the configured vaults. The first is the default for every tool.", readOnly, s.listVaults)
	addTool(s, "list_notes", "List files in a folder of the vault, optionally recursively and filtered by a glob such as Projects/**/*.md. Paged.", readOnly, listNotes)
	addTool(s, "get_note_info", "Show a file's etag, size, modification time and, for notes, its properties with types, tags, heading outline with line numbers, block IDs, and link and backlink counts. No body text.", readOnly, getNoteInfo)
	addTool(s, "read_note", "Read a note or other text file: the whole file, one heading's section, one block by ^id, or a line range. Returns the file's etag, needed for edits.", readOnly, readNote)
	addTool(s, "read_notes", "Read up to 20 whole text files at once, each with its etag.", readOnly, readNotes)
	addTool(s, "search", "Full-text search of the notes, ranked by relevance (BM25). Every word must appear; \"quoted phrases\" must appear as written. Filters: tags (nested tags match), properties, path glob, modified since. Returns matching lines with line numbers. Paged.", readOnly, searchNotes)
	addTool(s, "list_tags", "List the vault's tags with note counts, including parent tags of nested tags. Paged.", readOnly, listTags)
	addTool(s, "find_by_property", "Find notes whose property has a value (a scalar, or any list item; case is ignored), or that have the property at all when value is omitted. Paged.", readOnly, findByProperty)
	addTool(s, "backlinks", "List the links from other files that point at a file, with source and line. Includes embeds, frontmatter links and canvas cards. Paged.", readOnly, backlinks)
	addTool(s, "outgoing_links", "List a file's links and embeds, each with the file it resolves to, or its candidates when ambiguous. Paged.", readOnly, outgoingLinks)
	addTool(s, "unresolved_links", "List links that point at no file, with source and line. Excluded files are skipped. Paged.", readOnly, unresolvedLinks)
	addTool(s, "orphans", "List files that no other file links to. Excluded files are skipped. Paged.", readOnly, orphans)
	addTool(s, "resolve_link", "Resolve a link the way Obsidian does, from the note it is written in. Shows every candidate when it is ambiguous.", readOnly, resolveLink)
	addTool(s, "list_trash", "List the files in the vault's .trash folder, with the path restore_from_trash takes. Paged.", readOnly, listTrash)
}

// --- list_vaults

type listVaultsIn struct{ VaultArg }

type vaultInfo struct {
	Name    string `json:"name"`
	Folder  string `json:"folder"`
	Default bool   `json:"default"`
}

type listVaultsOut struct {
	Vaults   []vaultInfo `json:"vaults"`
	ReadOnly bool        `json:"read_only"`
}

func (s *Server) listVaults(_ context.Context, _ *vaultState, _ listVaultsIn) (string, listVaultsOut, error) {
	var res listVaultsOut
	res.ReadOnly = s.cfg.ReadOnly
	o := newOut()
	for i, vs := range s.vaults {
		res.Vaults = append(res.Vaults, vaultInfo{Name: vs.name, Folder: vs.v.Dir(), Default: i == 0})
		def := ""
		if i == 0 {
			def = " (default)"
		}
		o.line("%s%s: %s", vs.name, def, vs.v.Dir())
	}
	if s.cfg.ReadOnly {
		o.line("Read-only: write tools are off.")
	}
	return o.String(), res, nil
}

// --- list_notes

type listNotesIn struct {
	VaultArg
	Folder    string `json:"folder,omitempty" jsonschema:"folder to list; default: the vault root"`
	Glob      string `json:"glob,omitempty" jsonschema:"keep paths matching this glob; ** matches any folders, matching ignores case"`
	Recursive *bool  `json:"recursive,omitempty" jsonschema:"descend into subfolders; default true"`
	PageArgs
}

type entryOut struct {
	Path     string    `json:"path"`
	Kind     string    `json:"kind"`
	Size     int64     `json:"size,omitempty"`
	Modified time.Time `json:"modified"`
}

type listNotesOut struct {
	Entries []entryOut `json:"entries"`
	Total   int        `json:"total"`
	Next    string     `json:"next_cursor,omitempty"`
}

func listNotes(ctx context.Context, vs *vaultState, in listNotesIn) (string, listNotesOut, error) {
	if in.Glob != "" {
		if _, err := vault.MatchGlob(in.Glob, ""); err != nil {
			return "", listNotesOut{}, err
		}
	}
	rec := in.Recursive == nil || *in.Recursive
	var all []entryOut
	for e, err := range vs.v.List(ctx, strings.Trim(in.Folder, "/"), rec) {
		if err != nil {
			return "", listNotesOut{}, err
		}
		if in.Glob != "" {
			if ok, _ := vault.MatchGlob(in.Glob, e.Path); !ok {
				continue
			}
		}
		all = append(all, entryOut{Path: e.Path, Kind: string(e.Kind), Size: e.Size, Modified: e.ModTime})
	}
	p, err := page(all, in.PageArgs, func(e entryOut) string {
		if e.Kind == string(vault.KindFolder) {
			return e.Path + "/"
		}
		return e.Path
	})
	if err != nil {
		return "", listNotesOut{}, err
	}
	o := newOut()
	o.line("%d of %d entries%s.", len(p.items), len(all), more(p.next))
	o.untrusted("paths", strings.Join(p.lines, "\n"))
	return o.String(), listNotesOut{Entries: nonNil(p.items), Total: len(all), Next: p.next}, nil
}

func more(next string) string {
	if next == "" {
		return ""
	}
	return fmt.Sprintf("; more with cursor %q", next)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// --- get_note_info

type pathIn struct {
	VaultArg
	Path string `json:"path" jsonschema:"vault-relative path, with extension"`
}

type propOut struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type headingOut struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

type noteInfoOut struct {
	Path       string       `json:"path"`
	Kind       string       `json:"kind"`
	ETag       string       `json:"etag"`
	Size       int          `json:"size"`
	Modified   time.Time    `json:"modified"`
	Lines      int          `json:"lines,omitempty"`
	Properties []propOut    `json:"properties,omitempty"`
	Tags       []string     `json:"tags,omitempty"`
	Outline    []headingOut `json:"outline,omitempty"`
	BlockIDs   []string     `json:"block_ids,omitempty"`
	LinksOut   int          `json:"links_out"`
	Backlinks  int          `json:"backlinks"`
}

func getNoteInfo(ctx context.Context, vs *vaultState, in pathIn) (string, noteInfoOut, error) {
	f, err := vs.v.Read(ctx, in.Path)
	if err != nil {
		return "", noteInfoOut{}, err
	}
	res := noteInfoOut{Path: f.Path, Kind: string(f.Kind), ETag: f.ETag, Size: len(f.Data), Modified: f.ModTime}
	var detail strings.Builder
	if f.Kind == vault.KindMarkdown {
		types, err := vs.v.PropertyTypes(ctx)
		if err != nil {
			return "", noteInfoOut{}, err
		}
		d := ofm.Parse(f.Data)
		fm := props.Read(d)
		res.Lines = countLines(f.Data)
		starts := lineStarts(f.Data)
		for i := range fm.Props {
			p := &fm.Props[i]
			res.Properties = append(res.Properties, propOut{Name: p.Name, Type: string(props.TypeOf(p, types)), Value: p.Value})
			fmt.Fprintf(&detail, "property %s (%s): %v\n", p.Name, props.TypeOf(p, types), p.Value)
		}
		if fm.Err != nil {
			fmt.Fprintf(&detail, "frontmatter error: %v\n", fm.Err)
		}
		res.Tags = noteTags(d, fm)
		if len(res.Tags) > 0 {
			fmt.Fprintf(&detail, "tags: %s\n", strings.Join(res.Tags, ", "))
		}
		for _, h := range d.Headings {
			line := lineAt(starts, h.Start)
			res.Outline = append(res.Outline, headingOut{Level: h.Level, Text: h.Text, Line: line})
			fmt.Fprintf(&detail, "%s %s (line %d)\n", strings.Repeat("#", h.Level), h.Text, line)
		}
		for _, b := range d.BlockIDs {
			res.BlockIDs = append(res.BlockIDs, b.ID)
		}
		if len(res.BlockIDs) > 0 {
			fmt.Fprintf(&detail, "block ids: ^%s\n", strings.Join(res.BlockIDs, ", ^"))
		}
	}
	outRefs, err := vs.links.Outgoing(ctx, f.Path)
	if err != nil {
		return "", noteInfoOut{}, err
	}
	back, err := vs.links.Backlinks(ctx, f.Path)
	if err != nil {
		return "", noteInfoOut{}, err
	}
	res.LinksOut, res.Backlinks = len(outRefs), len(back)
	o := newOut()
	o.line("%s: %s, %d bytes, modified %s, etag %s", res.Path, res.Kind, res.Size, res.Modified.Format(time.RFC3339), res.ETag)
	o.line("%d outgoing links, %d backlinks.", res.LinksOut, res.Backlinks)
	if detail.Len() > 0 {
		o.untrusted(fmt.Sprintf("info path=%q", res.Path), detail.String())
	}
	return o.String(), res, nil
}

// noteTags returns a note's tags as written, inline then frontmatter, each
// once ignoring case.
func noteTags(d *ofm.Doc, fm *props.Frontmatter) []string {
	var out []string
	seen := map[string]bool{}
	names := make([]string, 0, len(d.Tags))
	for _, t := range d.Tags {
		names = append(names, t.Name)
	}
	for _, n := range append(names, fm.Tags()...) {
		if k := ofm.NormTag(n); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, strings.TrimPrefix(n, "#"))
		}
	}
	return out
}

// --- read_note

type readNoteIn struct {
	VaultArg
	Path      string `json:"path" jsonschema:"vault-relative path, with extension"`
	Heading   string `json:"heading,omitempty" jsonschema:"read only this heading's section; use Parent#Child to disambiguate"`
	Block     string `json:"block,omitempty" jsonschema:"read only the block with this ^id"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"first line to read, 1-based"`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"last line to read, inclusive"`
}

type readNoteOut struct {
	Path      string `json:"path"`
	ETag      string `json:"etag"`
	Lines     int    `json:"lines"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Truncated bool   `json:"truncated"`
}

func readNote(ctx context.Context, vs *vaultState, in readNoteIn) (string, readNoteOut, error) {
	f, err := readText(ctx, vs, in.Path)
	if err != nil {
		return "", readNoteOut{}, err
	}
	starts := lineStarts(f.Data)
	span := ofm.Span{End: len(f.Data)}
	targets := 0
	for _, set := range []bool{in.Heading != "", in.Block != "", in.StartLine != 0 || in.EndLine != 0} {
		if set {
			targets++
		}
	}
	if targets > 1 {
		return "", readNoteOut{}, errors.New("pass only one of heading, block or a line range")
	}
	switch {
	case in.Heading != "":
		if span, err = vault.FindSection(f.Data, in.Heading); err != nil {
			return "", readNoteOut{}, err
		}
	case in.Block != "":
		if span, err = vault.FindBlock(f.Data, in.Block); err != nil {
			return "", readNoteOut{}, err
		}
	case in.StartLine != 0 || in.EndLine != 0:
		if span, err = lineSpan(f.Data, starts, in.StartLine, in.EndLine); err != nil {
			return "", readNoteOut{}, err
		}
	}
	text, cut := truncate(string(f.Data[span.Start:span.End]), noteBytes)
	res := readNoteOut{Path: f.Path, ETag: f.ETag, Lines: countLines(f.Data), StartLine: lineAt(starts, span.Start), Truncated: cut}
	res.EndLine = max(res.StartLine, lineAt(starts, span.Start+max(len(text)-1, 0)))
	o := newOut()
	o.line("%s lines %d-%d of %d, etag %s", res.Path, res.StartLine, res.EndLine, res.Lines, res.ETag)
	if cut {
		o.line("Truncated at %d KB; read the rest with start_line=%d.", noteBytes>>10, res.EndLine+1)
	}
	o.untrusted(fmt.Sprintf("path=%q", f.Path), text)
	return o.String(), res, nil
}

// readText reads a text file: a note, canvas or base.
func readText(ctx context.Context, vs *vaultState, p string) (*vault.File, error) {
	f, err := vs.v.Read(ctx, p)
	if err != nil {
		return nil, err
	}
	switch f.Kind {
	case vault.KindMarkdown, vault.KindCanvas, vault.KindBase:
		return f, nil
	}
	return nil, fmt.Errorf("%s is a %s file, not text; only .md, .canvas and .base files can be read", f.Path, f.Kind)
}

// lineSpan returns the bytes of lines from..to (1-based, inclusive; 0 means
// the first or last line).
func lineSpan(src []byte, starts []int, from, to int) (ofm.Span, error) {
	n := countLines(src)
	if from == 0 {
		from = 1
	}
	if to == 0 || to > n {
		to = n
	}
	if from < 1 || from > max(n, 1) || to < from {
		return ofm.Span{}, fmt.Errorf("line range %d-%d is outside the file's %d lines", from, to, n)
	}
	end := len(src)
	if to < len(starts) {
		end = starts[to]
	}
	return ofm.Span{Start: starts[from-1], End: end}, nil
}

func countLines(src []byte) int {
	n := bytes.Count(src, []byte("\n"))
	if len(src) > 0 && src[len(src)-1] != '\n' {
		n++
	}
	return n
}

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' && i+1 < len(src) {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// lineAt returns the 1-based line holding byte offset off.
func lineAt(starts []int, off int) int {
	n, found := slices.BinarySearch(starts, off)
	if found {
		return n + 1
	}
	return n
}

// --- read_notes

type readNotesIn struct {
	VaultArg
	Paths []string `json:"paths" jsonschema:"up to 20 vault-relative paths"`
}

type batchNote struct {
	Path      string `json:"path"`
	ETag      string `json:"etag,omitempty"`
	Lines     int    `json:"lines,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
}

type readNotesOut struct {
	Notes []batchNote `json:"notes"`
}

func readNotes(ctx context.Context, vs *vaultState, in readNotesIn) (string, readNotesOut, error) {
	if len(in.Paths) == 0 || len(in.Paths) > maxBatch {
		return "", readNotesOut{}, fmt.Errorf("pass 1 to %d paths", maxBatch)
	}
	var res readNotesOut
	o := newOut()
	budget := batchBytes
	for _, p := range in.Paths {
		f, err := readText(ctx, vs, p)
		if err != nil {
			res.Notes = append(res.Notes, batchNote{Path: p, Error: toolError(err).Error()})
			o.line("%s: error: %v", p, toolError(err))
			continue
		}
		if budget <= 0 {
			res.Notes = append(res.Notes, batchNote{Path: f.Path, Error: "skipped: the batch reached its size cap; read it separately"})
			o.line("%s: skipped, the batch reached its size cap; read it separately.", f.Path)
			continue
		}
		text, cut := truncate(string(f.Data), budget)
		budget -= len(text)
		bn := batchNote{Path: f.Path, ETag: f.ETag, Lines: countLines(f.Data), Truncated: cut}
		res.Notes = append(res.Notes, bn)
		note := ""
		if cut {
			note = " (truncated; read the rest with read_note and start_line)"
		}
		o.line("%s: %d lines, etag %s%s", bn.Path, bn.Lines, bn.ETag, note)
		o.untrusted(fmt.Sprintf("path=%q", f.Path), text)
	}
	return o.String(), res, nil
}

// --- search

type searchIn struct {
	VaultArg
	Query         string            `json:"query,omitempty" jsonschema:"words that must all appear, and \"quoted phrases\"; omit to list notes matching the filters"`
	Tags          []string          `json:"tags,omitempty" jsonschema:"tags the note must have; nested tags match (inbox matches inbox/to-read)"`
	Properties    map[string]string `json:"properties,omitempty" jsonschema:"property name to value; an empty value only requires the property"`
	Path          string            `json:"path,omitempty" jsonschema:"glob the note path must match, such as Projects/**"`
	ModifiedSince string            `json:"modified_since,omitempty" jsonschema:"RFC 3339 time or YYYY-MM-DD"`
	Cursor        string            `json:"cursor,omitempty" jsonschema:"the next_cursor of the previous page"`
	Limit         int               `json:"limit,omitempty" jsonschema:"page size; default 20, at most 100"`
}

type hitOut struct {
	Path     string    `json:"path"`
	Score    float64   `json:"score,omitempty"`
	Modified time.Time `json:"modified"`
	Lines    []int     `json:"lines,omitempty"`
}

type searchOut struct {
	Results []hitOut `json:"results"`
	Total   int      `json:"total"`
	Next    string   `json:"next_cursor,omitempty"`
}

func searchNotes(ctx context.Context, vs *vaultState, in searchIn) (string, searchOut, error) {
	q := search.Query{Text: in.Query, Tags: in.Tags, Props: in.Properties, Path: in.Path, Cursor: in.Cursor, Limit: in.Limit}
	if in.ModifiedSince != "" {
		t, err := parseSince(in.ModifiedSince)
		if err != nil {
			return "", searchOut{}, err
		}
		q.Since = t
	}
	return runSearch(ctx, vs, q)
}

func runSearch(ctx context.Context, vs *vaultState, q search.Query) (string, searchOut, error) {
	p, err := vs.search.Search(ctx, q)
	if errors.Is(err, search.ErrBadCursor) {
		return "", searchOut{}, errBadCursor
	}
	if err != nil {
		return "", searchOut{}, err
	}
	res := searchOut{Results: []hitOut{}, Total: p.Total, Next: p.Next}
	var body strings.Builder
	for _, r := range p.Results {
		h := hitOut{Path: r.Path, Score: r.Score, Modified: r.ModTime}
		fmt.Fprintf(&body, "%s\n", r.Path)
		for _, sn := range r.Snippets {
			h.Lines = append(h.Lines, sn.Line)
			fmt.Fprintf(&body, "  %d: %s\n", sn.Line, sn.Text)
		}
		res.Results = append(res.Results, h)
	}
	o := newOut()
	o.line("%d of %d matching notes%s.", len(p.Results), p.Total, more(p.Next))
	if body.Len() > 0 {
		o.untrusted("results", body.String())
	}
	return o.String(), res, nil
}

func parseSince(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation(time.DateOnly, s, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("modified_since %q is not an RFC 3339 time or YYYY-MM-DD", s)
}

// --- find_by_property

type findByPropertyIn struct {
	VaultArg
	Name   string `json:"name" jsonschema:"property name"`
	Value  string `json:"value,omitempty" jsonschema:"value to match; omit to find notes that have the property"`
	Path   string `json:"path,omitempty" jsonschema:"glob the note path must match"`
	Cursor string `json:"cursor,omitempty" jsonschema:"the next_cursor of the previous page"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size; default 20, at most 100"`
}

func findByProperty(ctx context.Context, vs *vaultState, in findByPropertyIn) (string, searchOut, error) {
	if strings.TrimSpace(in.Name) == "" {
		return "", searchOut{}, errors.New("name is required")
	}
	return runSearch(ctx, vs, search.Query{Props: map[string]string{in.Name: in.Value}, Path: in.Path, Cursor: in.Cursor, Limit: in.Limit})
}

// --- list_tags

type listTagsIn struct {
	VaultArg
	Prefix string `json:"prefix,omitempty" jsonschema:"only tags starting with this, ignoring case (projects/ lists the nested tags of projects)"`
	PageArgs
}

type tagOut struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Parent string `json:"parent,omitempty"`
}

type listTagsOut struct {
	Tags  []tagOut `json:"tags"`
	Total int      `json:"total"`
	Next  string   `json:"next_cursor,omitempty"`
}

func listTags(ctx context.Context, vs *vaultState, in listTagsIn) (string, listTagsOut, error) {
	tags, err := vs.search.Tags(ctx)
	if err != nil {
		return "", listTagsOut{}, err
	}
	prefix := strings.ToLower(strings.TrimPrefix(in.Prefix, "#"))
	var all []tagOut
	for _, t := range tags {
		if strings.HasPrefix(strings.ToLower(t.Name), prefix) {
			all = append(all, tagOut(t))
		}
	}
	p, err := page(all, in.PageArgs, func(t tagOut) string { return fmt.Sprintf("#%s %d", t.Name, t.Count) })
	if err != nil {
		return "", listTagsOut{}, err
	}
	o := newOut()
	o.line("%d of %d tags%s.", len(p.items), len(all), more(p.next))
	o.untrusted("tags", strings.Join(p.lines, "\n"))
	return o.String(), listTagsOut{Tags: nonNil(p.items), Total: len(all), Next: p.next}, nil
}

// --- links

type pathPageIn struct {
	VaultArg
	Path string `json:"path" jsonschema:"vault-relative path, with extension"`
	PageArgs
}

type linkOut struct {
	Source     string   `json:"source"`
	Line       int      `json:"line,omitempty"`
	Kind       string   `json:"kind"`
	Property   string   `json:"property,omitempty"`
	Node       string   `json:"canvas_node,omitempty"`
	Link       string   `json:"link"`
	Target     string   `json:"target"`
	Subpath    string   `json:"subpath,omitempty"`
	Embed      bool     `json:"embed,omitempty"`
	Resolved   string   `json:"resolved,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

type linksOut struct {
	Links []linkOut `json:"links"`
	Total int       `json:"total"`
	Next  string    `json:"next_cursor,omitempty"`
}

func toLinkOut(r links.Ref) linkOut {
	return linkOut{
		Source: r.Source, Line: r.Line, Kind: string(r.Kind), Property: r.Property, Node: r.Node,
		Link: linkText(r), Target: r.Link.Target, Subpath: r.Link.Subpath, Embed: r.Link.Embed,
		Resolved: r.Resolved, Candidates: r.Candidates,
	}
}

// linkText shows a link the way it is written, rebuilt from its parts.
func linkText(r links.Ref) string {
	l := r.Link
	if r.Kind == links.KindCanvas {
		return "canvas card " + l.Target + l.Subpath
	}
	bang := ""
	if l.Embed {
		bang = "!"
	}
	if l.Markdown {
		return fmt.Sprintf("%s[%s](%s%s)", bang, l.Display, l.Target, l.Subpath)
	}
	disp := ""
	if l.HasDisplay {
		disp = "|" + l.Display
	}
	return fmt.Sprintf("%s[[%s%s%s]]", bang, l.Target, l.Subpath, disp)
}

func renderLink(l linkOut) string {
	where := l.Source
	if l.Line > 0 {
		where = fmt.Sprintf("%s:%d", l.Source, l.Line)
	}
	switch {
	case l.Resolved != "":
		return fmt.Sprintf("%s %s -> %s", where, l.Link, l.Resolved)
	case len(l.Candidates) > 0:
		return fmt.Sprintf("%s %s -> ambiguous: %s", where, l.Link, strings.Join(l.Candidates, ", "))
	}
	return fmt.Sprintf("%s %s -> unresolved", where, l.Link)
}

func linkPage(refs []links.Ref, a PageArgs, label string) (string, linksOut, error) {
	all := make([]linkOut, 0, len(refs))
	for _, r := range refs {
		all = append(all, toLinkOut(r))
	}
	p, err := page(all, a, renderLink)
	if err != nil {
		return "", linksOut{}, err
	}
	o := newOut()
	o.line("%d of %d %s%s.", len(p.items), len(all), label, more(p.next))
	if len(p.lines) > 0 {
		o.untrusted("links", strings.Join(p.lines, "\n"))
	}
	return o.String(), linksOut{Links: nonNil(p.items), Total: len(all), Next: p.next}, nil
}

func backlinks(ctx context.Context, vs *vaultState, in pathPageIn) (string, linksOut, error) {
	refs, err := vs.links.Backlinks(ctx, in.Path)
	if err != nil {
		return "", linksOut{}, err
	}
	return linkPage(refs, in.PageArgs, "backlinks")
}

func outgoingLinks(ctx context.Context, vs *vaultState, in pathPageIn) (string, linksOut, error) {
	refs, err := vs.links.Outgoing(ctx, in.Path)
	if err != nil {
		return "", linksOut{}, err
	}
	return linkPage(refs, in.PageArgs, "outgoing links")
}

type pageIn struct {
	VaultArg
	PageArgs
}

func unresolvedLinks(ctx context.Context, vs *vaultState, in pageIn) (string, linksOut, error) {
	refs, err := vs.links.Unresolved(ctx)
	if err != nil {
		return "", linksOut{}, err
	}
	return linkPage(refs, in.PageArgs, "unresolved links")
}

type orphansIn struct {
	VaultArg
	NotesOnly bool `json:"notes_only,omitempty" jsonschema:"list only .md notes, not attachments, canvases or bases"`
	PageArgs
}

type pathsOut struct {
	Paths []string `json:"paths"`
	Total int      `json:"total"`
	Next  string   `json:"next_cursor,omitempty"`
}

func orphans(ctx context.Context, vs *vaultState, in orphansIn) (string, pathsOut, error) {
	all, err := vs.links.Orphans(ctx)
	if err != nil {
		return "", pathsOut{}, err
	}
	if in.NotesOnly {
		all = slices.DeleteFunc(all, func(p string) bool { return vault.KindOf(p) != vault.KindMarkdown })
	}
	p, err := page(all, in.PageArgs, func(s string) string { return s })
	if err != nil {
		return "", pathsOut{}, err
	}
	o := newOut()
	o.line("%d of %d orphans%s.", len(p.items), len(all), more(p.next))
	if len(p.lines) > 0 {
		o.untrusted("paths", strings.Join(p.lines, "\n"))
	}
	return o.String(), pathsOut{Paths: nonNil(p.items), Total: len(all), Next: p.next}, nil
}

// --- resolve_link

type resolveLinkIn struct {
	VaultArg
	Link string `json:"link" jsonschema:"the link as written ([[Note#Heading|text]] or [text](Note.md)) or a bare target (Note)"`
	From string `json:"from,omitempty" jsonschema:"path of the note the link is written in; default: a note at the vault root"`
}

type resolveLinkOut struct {
	Resolved   string   `json:"resolved,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	Status     string   `json:"status" jsonschema:"resolved, ambiguous or unresolved"`
}

func resolveLink(ctx context.Context, vs *vaultState, in resolveLinkIn) (string, resolveLinkOut, error) {
	from := in.From
	if from != "" {
		c, err := vault.Clean(from)
		if err != nil {
			return "", resolveLinkOut{}, err
		}
		from = c
	}
	p, err := vs.links.Resolve(ctx, from, in.Link)
	var ae *links.AmbiguousError
	o := newOut()
	switch {
	case errors.As(err, &ae):
		o.line("Ambiguous: %d files match.", len(ae.Candidates))
		o.untrusted("candidates", strings.Join(ae.Candidates, "\n"))
		return o.String(), resolveLinkOut{Candidates: ae.Candidates, Status: "ambiguous"}, nil
	case err != nil:
		return "", resolveLinkOut{}, err
	case p == "":
		o.line("Unresolved: no file matches.")
		return o.String(), resolveLinkOut{Status: "unresolved"}, nil
	}
	o.line("Resolved.")
	o.untrusted("path", p)
	return o.String(), resolveLinkOut{Resolved: p, Status: "resolved"}, nil
}

// --- list_trash

type trashOut struct {
	TrashPath    string    `json:"trash_path"`
	OriginalPath string    `json:"original_path"`
	Size         int64     `json:"size"`
	Modified     time.Time `json:"modified"`
}

type listTrashOut struct {
	Files []trashOut `json:"files"`
	Total int        `json:"total"`
	Next  string     `json:"next_cursor,omitempty"`
}

func listTrash(ctx context.Context, vs *vaultState, in pageIn) (string, listTrashOut, error) {
	var all []trashOut
	for e, err := range vs.v.ListTrash(ctx) {
		if err != nil {
			return "", listTrashOut{}, err
		}
		all = append(all, trashOut{TrashPath: e.TrashPath, OriginalPath: e.OriginalPath, Size: e.Size, Modified: e.ModTime})
	}
	p, err := page(all, in.PageArgs, func(t trashOut) string { return t.TrashPath + " (from " + t.OriginalPath + ")" })
	if err != nil {
		return "", listTrashOut{}, err
	}
	o := newOut()
	o.line("%d of %d files in the trash%s.", len(p.items), len(all), more(p.next))
	if len(p.lines) > 0 {
		o.untrusted("paths", strings.Join(p.lines, "\n"))
	}
	return o.String(), listTrashOut{Files: nonNil(p.items), Total: len(all), Next: p.next}, nil
}
