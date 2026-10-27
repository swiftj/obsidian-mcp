package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/swiftj/obsidian-mcp/vault"
	"github.com/swiftj/obsidian-mcp/vault/bases"
	"github.com/swiftj/obsidian-mcp/vault/canvas"
	"github.com/swiftj/obsidian-mcp/vault/ofm"
)

func (s *Server) addCanvasTools() {
	addTool(s, "read_canvas", "Read a canvas (.canvas, JSON Canvas 1.0): its nodes (z-order, back to front) and edges, any spec problems, the raw JSON and the etag edit_canvas needs. Paged over nodes, then edges.", readOnly, readCanvas)
	addTool(s, "read_base", "Read a base (.base): its views, filters, formulas, properties and summaries, any syntax problems, the YAML and the etag edit_base needs. For a note (.md), validates each ```base code block. Queries are never run.", readOnly, readBase)
	if s.cfg.ReadOnly {
		return
	}
	addTool(s, "edit_canvas", "Edit a canvas with ops, applied in order: add_node, update_node, remove_node (also removes its edges), add_edge, update_edge, remove_edge. Only the edited nodes and edges change; the result is checked against JSON Canvas 1.0. Needs the etag from read_canvas.", destructive, editCanvas)
	addTool(s, "create_canvas", "Create a new canvas (.canvas), empty or with add_node and add_edge ops. Fails if the file exists.", additive, createCanvas)
	addTool(s, "create_base", "Create a new base (.base) from YAML text, checked against the Bases syntax. Fails if the file exists.", additive, createBase)
	addTool(s, "edit_base", "Edit a base's YAML with ops, applied in order: set (a value at a path, creating missing keys), remove, insert (into a list). A path is a list of keys and indexes, such as [\"views\", \"My table\", \"limit\"]; under views a name picks a view. Only the changed entries are rewritten. Needs the etag from read_base.", destructive, editBase)
}

func onlyExt(op, p, ext string) error {
	if strings.ToLower(path.Ext(p)) != ext {
		return &vault.PathError{Op: op, Path: p, Reason: "this tool works on " + ext + " files only"}
	}
	return nil
}

// short shortens vault text to one line of at most n runes.
func short(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// --- read_canvas

type readCanvasIn struct {
	VaultArg
	Path        string `json:"path" jsonschema:"vault-relative path ending in .canvas"`
	IncludeJSON *bool  `json:"include_json,omitempty" jsonschema:"also return the raw JSON; default true"`
	PageArgs
}

type readCanvasOut struct {
	Path       string           `json:"path"`
	ETag       string           `json:"etag"`
	TotalNodes int              `json:"total_nodes"`
	TotalEdges int              `json:"total_edges"`
	Nodes      []canvas.Node    `json:"nodes"`
	Edges      []canvas.Edge    `json:"edges"`
	Problems   []canvas.Problem `json:"problems,omitempty"`
	Next       string           `json:"next_cursor,omitempty"`
}

// canvasItem is a node or an edge, for paging both as one list.
type canvasItem struct {
	node *canvas.Node
	edge *canvas.Edge
}

func nodeLine(n canvas.Node) string {
	what := ""
	switch n.Type {
	case "text":
		what = fmt.Sprintf("%q", short(n.Text, 80))
	case "file":
		what = n.File + n.Subpath
	case "link":
		what = n.URL
	case "group":
		what = fmt.Sprintf("%q", short(n.Label, 80))
	}
	l := fmt.Sprintf("node %s %s (%d,%d %dx%d) %s", n.ID, n.Type, n.X, n.Y, n.Width, n.Height, what)
	if n.Color != "" {
		l += " color " + n.Color
	}
	return l
}

func edgeLine(e canvas.Edge) string {
	l := fmt.Sprintf("edge %s %s → %s", e.ID, e.FromNode, e.ToNode)
	if e.Label != "" {
		l += fmt.Sprintf(" %q", short(e.Label, 80))
	}
	return l
}

func readCanvas(ctx context.Context, vs *vaultState, in readCanvasIn) (string, readCanvasOut, error) {
	if err := onlyExt("read", in.Path, ".canvas"); err != nil {
		return "", readCanvasOut{}, err
	}
	f, err := vs.v.Read(ctx, in.Path)
	if err != nil {
		return "", readCanvasOut{}, err
	}
	c, err := canvas.Parse(f.Data)
	if err != nil {
		return "", readCanvasOut{}, fmt.Errorf("%s: %w", f.Path, err)
	}
	nodes, edges := c.Nodes(), c.Edges()
	items := make([]canvasItem, 0, len(nodes)+len(edges))
	for i := range nodes {
		items = append(items, canvasItem{node: &nodes[i]})
	}
	for i := range edges {
		items = append(items, canvasItem{edge: &edges[i]})
	}
	p, err := page(items, in.PageArgs, func(it canvasItem) string {
		if it.node != nil {
			return nodeLine(*it.node)
		}
		return edgeLine(*it.edge)
	})
	if err != nil {
		return "", readCanvasOut{}, err
	}
	res := readCanvasOut{Path: f.Path, ETag: f.ETag, TotalNodes: len(nodes), TotalEdges: len(edges),
		Nodes: []canvas.Node{}, Edges: []canvas.Edge{}, Problems: c.Validate(), Next: p.next}
	for _, it := range p.items {
		if it.node != nil {
			res.Nodes = append(res.Nodes, *it.node)
		} else {
			res.Edges = append(res.Edges, *it.edge)
		}
	}
	o := newOut()
	o.line("%s: %d nodes, %d edges, %d problems, etag %s", f.Path, len(nodes), len(edges), len(res.Problems), f.ETag)
	if p.next != "" {
		o.line("More: call again with cursor=%s.", p.next)
	}
	body := strings.Join(p.lines, "\n")
	for _, pr := range res.Problems {
		body += "\nproblem: " + pr.String()
	}
	o.untrusted(fmt.Sprintf("canvas path=%q", f.Path), body)
	if in.IncludeJSON == nil || *in.IncludeJSON {
		text, cut := truncate(string(f.Data), noteBytes)
		if cut {
			o.line("JSON truncated at %d KB.", noteBytes>>10)
		}
		o.untrusted(fmt.Sprintf("json path=%q", f.Path), text)
	}
	return o.String(), res, nil
}

// --- edit_canvas and create_canvas

type canvasOpIn struct {
	Op              string   `json:"op" jsonschema:"add_node, update_node, remove_node, add_edge, update_edge or remove_edge"`
	ID              string   `json:"id,omitempty" jsonschema:"the node or edge ID; optional for adds, which then get a new unique ID"`
	Type            *string  `json:"type,omitempty" jsonschema:"node type: text, file, link or group"`
	X               *int     `json:"x,omitempty"`
	Y               *int     `json:"y,omitempty"`
	Width           *int     `json:"width,omitempty"`
	Height          *int     `json:"height,omitempty"`
	Color           *string  `json:"color,omitempty" jsonschema:"a preset \"1\" (red) to \"6\" (purple), or a hex colour like \"#FF0000\""`
	Text            *string  `json:"text,omitempty" jsonschema:"text nodes: Markdown text"`
	File            *string  `json:"file,omitempty" jsonschema:"file nodes: vault-relative path of the file"`
	Subpath         *string  `json:"subpath,omitempty" jsonschema:"file nodes: #Heading or #^block"`
	URL             *string  `json:"url,omitempty" jsonschema:"link nodes: the URL"`
	Label           *string  `json:"label,omitempty" jsonschema:"group nodes and edges: the label"`
	Background      *string  `json:"background,omitempty" jsonschema:"group nodes: background image path"`
	BackgroundStyle *string  `json:"backgroundStyle,omitempty" jsonschema:"group nodes: cover, ratio or repeat"`
	FromNode        *string  `json:"fromNode,omitempty" jsonschema:"edges: the start node ID"`
	FromSide        *string  `json:"fromSide,omitempty" jsonschema:"edges: top, right, bottom or left"`
	FromEnd         *string  `json:"fromEnd,omitempty" jsonschema:"edges: none (default) or arrow"`
	ToNode          *string  `json:"toNode,omitempty" jsonschema:"edges: the end node ID"`
	ToSide          *string  `json:"toSide,omitempty" jsonschema:"edges: top, right, bottom or left"`
	ToEnd           *string  `json:"toEnd,omitempty" jsonschema:"edges: none or arrow (default)"`
	Unset           []string `json:"unset,omitempty" jsonschema:"updates: fields to remove, such as color"`
}

func (op canvasOpIn) toOp() (canvas.Op, error) {
	c := canvas.Op{Kind: canvas.OpKind(op.Op), ID: op.ID, Set: map[string]any{}, Unset: op.Unset}
	for k, v := range map[string]*int{"x": op.X, "y": op.Y, "width": op.Width, "height": op.Height} {
		if v != nil {
			c.Set[k] = *v
		}
	}
	for k, v := range map[string]*string{"type": op.Type, "color": op.Color, "text": op.Text, "file": op.File,
		"subpath": op.Subpath, "url": op.URL, "label": op.Label, "background": op.Background,
		"backgroundStyle": op.BackgroundStyle, "fromNode": op.FromNode, "fromSide": op.FromSide,
		"fromEnd": op.FromEnd, "toNode": op.ToNode, "toSide": op.ToSide, "toEnd": op.ToEnd} {
		if v != nil {
			c.Set[k] = *v
		}
	}
	for _, k := range []string{"file", "background"} {
		if p, ok := c.Set[k].(string); ok && p != "" {
			if _, err := vault.Clean(p); err != nil {
				return c, fmt.Errorf("%s: %w", k, err)
			}
		}
	}
	return c, nil
}

func canvasOps(in []canvasOpIn) ([]canvas.Op, error) {
	ops := make([]canvas.Op, 0, len(in))
	for i, op := range in {
		c, err := op.toOp()
		if err != nil {
			return nil, fmt.Errorf("ops[%d]: %w", i, err)
		}
		ops = append(ops, c)
	}
	return ops, nil
}

type editCanvasIn struct {
	VaultArg
	Path string       `json:"path" jsonschema:"vault-relative path ending in .canvas"`
	ETag string       `json:"etag" jsonschema:"the etag from your latest read_canvas"`
	Ops  []canvasOpIn `json:"ops" jsonschema:"edits, applied in order"`
	DryRunArg
}

type editCanvasOut struct {
	writeOut
	Added        []string         `json:"added,omitempty" jsonschema:"IDs of the added nodes and edges, in op order"`
	RemovedEdges []string         `json:"removed_edges,omitempty" jsonschema:"edges removed with their nodes"`
	Existing     []canvas.Problem `json:"existing_problems,omitempty" jsonschema:"problems already in the file, which didn't block the edit"`
}

func canvasResult(verb string, r *vault.Result, cr *canvas.Result) (string, editCanvasOut, error) {
	text, w, err := writeResult(verb, r)
	if err != nil {
		return "", editCanvasOut{}, err
	}
	out := editCanvasOut{writeOut: w, Added: cr.Added, RemovedEdges: cr.RemovedEdges, Existing: cr.Existing}
	var extra []string
	if len(cr.Added) > 0 {
		extra = append(extra, "added: "+strings.Join(cr.Added, ", "))
	}
	if len(cr.RemovedEdges) > 0 {
		extra = append(extra, "removed edges: "+strings.Join(cr.RemovedEdges, ", "))
	}
	for _, p := range cr.Existing {
		extra = append(extra, "existing problem: "+p.String())
	}
	if len(extra) > 0 {
		o := newOut()
		o.untrusted("canvas ids", strings.Join(extra, "\n"))
		text += o.String()
	}
	return text, out, nil
}

func editCanvas(ctx context.Context, vs *vaultState, in editCanvasIn) (string, editCanvasOut, error) {
	if err := onlyExt("edit", in.Path, ".canvas"); err != nil {
		return "", editCanvasOut{}, err
	}
	if len(in.Ops) == 0 {
		return "", editCanvasOut{}, errors.New("ops is empty")
	}
	ops, err := canvasOps(in.Ops)
	if err != nil {
		return "", editCanvasOut{}, err
	}
	var cr *canvas.Result
	r, err := vs.v.Update(ctx, in.Path, in.ETag, func(cur []byte) ([]byte, error) {
		var err error
		cr, err = canvas.Apply(cur, ops)
		if err != nil {
			return nil, err
		}
		return cr.Data, nil
	}, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", editCanvasOut{}, err
	}
	return canvasResult("edited", r, cr)
}

type createCanvasIn struct {
	VaultArg
	Path string       `json:"path" jsonschema:"vault-relative path ending in .canvas"`
	Ops  []canvasOpIn `json:"ops,omitempty" jsonschema:"add_node and add_edge ops for the first content; default: an empty canvas"`
	DryRunArg
}

func createCanvas(ctx context.Context, vs *vaultState, in createCanvasIn) (string, editCanvasOut, error) {
	if err := onlyExt("create", in.Path, ".canvas"); err != nil {
		return "", editCanvasOut{}, err
	}
	ops, err := canvasOps(in.Ops)
	if err != nil {
		return "", editCanvasOut{}, err
	}
	cr, err := canvas.New(ops)
	if err != nil {
		return "", editCanvasOut{}, err
	}
	r, err := vs.v.Create(ctx, in.Path, cr.Data, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", editCanvasOut{}, err
	}
	return canvasResult("created", r, cr)
}

// --- read_base

type readBaseIn struct {
	VaultArg
	Path string `json:"path" jsonschema:"vault-relative path ending in .base, or a note (.md) holding base code blocks"`
}

type baseSummary struct {
	Views      []bases.View    `json:"views,omitempty"`
	Filters    bool            `json:"filters,omitempty" jsonschema:"whether the base has top-level filters"`
	Formulas   []bases.Named   `json:"formulas,omitempty"`
	Properties []string        `json:"properties,omitempty"`
	Summaries  []bases.Named   `json:"summaries,omitempty"`
	Problems   []bases.Problem `json:"problems,omitempty"`
}

type baseBlockOut struct {
	Line int `json:"line" jsonschema:"line of the opening fence"`
	baseSummary
}

type readBaseOut struct {
	Path string `json:"path"`
	ETag string `json:"etag"`
	*baseSummary
	Blocks []baseBlockOut `json:"blocks,omitempty" jsonschema:"notes: the base code blocks"`
}

func summarize(b *bases.Base) *baseSummary {
	return &baseSummary{Views: b.Views(), Filters: b.HasFilters(), Formulas: b.Formulas(),
		Properties: b.Properties(), Summaries: b.Summaries(), Problems: b.Validate()}
}

func (s *baseSummary) lines() []string {
	var l []string
	for _, v := range s.Views {
		line := fmt.Sprintf("view %q (%s)", v.Name, v.Type)
		if v.Limit > 0 {
			line += fmt.Sprintf(" limit %d", v.Limit)
		}
		if v.GroupBy != "" {
			line += " grouped by " + v.GroupBy + " " + v.Direction
		}
		if v.Filtered {
			line += " with filters"
		}
		l = append(l, line)
	}
	if s.Filters {
		l = append(l, "top-level filters")
	}
	for _, f := range s.Formulas {
		l = append(l, fmt.Sprintf("formula %s: %s", f.Name, short(f.Value, 120)))
	}
	if len(s.Properties) > 0 {
		l = append(l, "properties: "+strings.Join(s.Properties, ", "))
	}
	for _, f := range s.Summaries {
		l = append(l, fmt.Sprintf("summary %s: %s", f.Name, short(f.Value, 120)))
	}
	for _, p := range s.Problems {
		l = append(l, "problem: "+p.String())
	}
	return l
}

func readBase(ctx context.Context, vs *vaultState, in readBaseIn) (string, readBaseOut, error) {
	ext := strings.ToLower(path.Ext(in.Path))
	if ext != ".base" && ext != ".md" {
		return "", readBaseOut{}, &vault.PathError{Op: "read", Path: in.Path, Reason: "this tool reads .base files and notes with base code blocks"}
	}
	f, err := vs.v.Read(ctx, in.Path)
	if err != nil {
		return "", readBaseOut{}, err
	}
	res := readBaseOut{Path: f.Path, ETag: f.ETag}
	o := newOut()
	if ext == ".md" {
		var body []string
		for _, blk := range bases.CodeBlocks(f.Data, ofm.Parse(f.Data)) {
			bo := baseBlockOut{Line: blk.Line}
			if b, err := bases.Parse([]byte(blk.Text)); err != nil {
				bo.Problems = []bases.Problem{{Msg: err.Error()}}
			} else {
				bo.baseSummary = *summarize(b)
			}
			res.Blocks = append(res.Blocks, bo)
			body = append(body, fmt.Sprintf("base block at line %d:", blk.Line))
			for _, l := range bo.lines() {
				body = append(body, "  "+l)
			}
		}
		o.line("%s: %d base code blocks, etag %s", f.Path, len(res.Blocks), f.ETag)
		if len(body) > 0 {
			o.untrusted(fmt.Sprintf("bases path=%q", f.Path), strings.Join(body, "\n"))
		}
		return o.String(), res, nil
	}
	b, err := bases.Parse(f.Data)
	if err != nil {
		return "", readBaseOut{}, fmt.Errorf("%s: %w", f.Path, err)
	}
	res.baseSummary = summarize(b)
	o.line("%s: %d views, %d problems, etag %s", f.Path, len(res.Views), len(res.Problems), f.ETag)
	if l := res.lines(); len(l) > 0 {
		o.untrusted(fmt.Sprintf("base path=%q", f.Path), strings.Join(l, "\n"))
	}
	text, cut := truncate(string(f.Data), noteBytes)
	if cut {
		o.line("YAML truncated at %d KB.", noteBytes>>10)
	}
	o.untrusted(fmt.Sprintf("yaml path=%q", f.Path), text)
	return o.String(), res, nil
}

// --- create_base and edit_base

type createBaseIn struct {
	VaultArg
	Path    string `json:"path" jsonschema:"vault-relative path ending in .base"`
	Content string `json:"content" jsonschema:"the base's YAML: filters, formulas, properties, summaries and views"`
	DryRunArg
}

type baseWriteOut struct {
	writeOut
	Problems []bases.Problem `json:"problems,omitempty" jsonschema:"warnings, and problems already in the file, which didn't block the write"`
}

func baseResult(verb string, r *vault.Result, ps []bases.Problem) (string, baseWriteOut, error) {
	text, w, err := writeResult(verb, r)
	if err != nil {
		return "", baseWriteOut{}, err
	}
	if len(ps) > 0 {
		var l []string
		for _, p := range ps {
			l = append(l, p.String())
		}
		o := newOut()
		o.untrusted("base problems", strings.Join(l, "\n"))
		text += o.String()
	}
	return text, baseWriteOut{writeOut: w, Problems: ps}, nil
}

func createBase(ctx context.Context, vs *vaultState, in createBaseIn) (string, baseWriteOut, error) {
	if err := onlyExt("create", in.Path, ".base"); err != nil {
		return "", baseWriteOut{}, err
	}
	b, err := bases.Parse([]byte(in.Content))
	if err != nil {
		return "", baseWriteOut{}, err
	}
	var warnings, blocking []bases.Problem
	for _, p := range b.Validate() {
		if p.Warning {
			warnings = append(warnings, p)
		} else {
			blocking = append(blocking, p)
		}
	}
	if len(blocking) > 0 {
		return "", baseWriteOut{}, &bases.Error{Msg: "the content isn't a valid base", Problems: blocking}
	}
	r, err := vs.v.Create(ctx, in.Path, []byte(in.Content), vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", baseWriteOut{}, err
	}
	return baseResult("created", r, warnings)
}

type baseOpIn struct {
	Op    string `json:"op" jsonschema:"set, remove or insert"`
	Path  []any  `json:"path" jsonschema:"keys and list indexes, such as [\"formulas\", \"ppu\"] or [\"views\", 0, \"limit\"]; under views a name picks a view"`
	Index *int   `json:"index,omitempty" jsonschema:"insert: the index to insert before; default: append"`
	Value any    `json:"value,omitempty" jsonschema:"set and insert: the value, as JSON (text, number, list or object)"`
}

type editBaseIn struct {
	VaultArg
	Path string     `json:"path" jsonschema:"vault-relative path ending in .base"`
	ETag string     `json:"etag" jsonschema:"the etag from your latest read_base"`
	Ops  []baseOpIn `json:"ops" jsonschema:"edits, applied in order"`
	DryRunArg
}

func editBase(ctx context.Context, vs *vaultState, in editBaseIn) (string, baseWriteOut, error) {
	if err := onlyExt("edit", in.Path, ".base"); err != nil {
		return "", baseWriteOut{}, err
	}
	if len(in.Ops) == 0 {
		return "", baseWriteOut{}, errors.New("ops is empty")
	}
	ops := make([]bases.Op, len(in.Ops))
	for i, op := range in.Ops {
		ops[i] = bases.Op{Kind: bases.OpKind(op.Op), Path: op.Path, Index: -1, Value: op.Value}
		if op.Index != nil {
			ops[i].Index = *op.Index
		}
	}
	var br *bases.Result
	r, err := vs.v.Update(ctx, in.Path, in.ETag, func(cur []byte) ([]byte, error) {
		var err error
		br, err = bases.Apply(cur, ops)
		if err != nil {
			return nil, err
		}
		return br.Data, nil
	}, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", baseWriteOut{}, err
	}
	return baseResult("edited", r, br.Existing)
}
