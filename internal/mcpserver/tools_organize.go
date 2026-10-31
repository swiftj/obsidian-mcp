package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/swiftj/obsidian-mcp/vault"
	"github.com/swiftj/obsidian-mcp/vault/daily"
	"github.com/swiftj/obsidian-mcp/vault/links"
)

// maxAttachment caps attach_file [policy D-012].
const maxAttachment = 100 << 20

func (s *Server) addOrganizeTools() {
	addTool(s, "move_note", "Move a note or other file to another path, rewriting every link that would break (wikilinks, Markdown links, embeds, frontmatter and canvas links) in the vault's link format. All files change or none. Use dry_run to see every changed link first.", destructive, moveNote)
	addTool(s, "rename_note", "Rename a note or other file in its folder, rewriting every link to it like move_note.", destructive, renameNote)
	addTool(s, "move_folder", "Move a folder and everything in it, rewriting links like move_note. The destination must not exist.", destructive, moveFolder)
	addTool(s, "create_folder", "Create a folder and any missing parents.", additive, createFolder)
	addTool(s, "rename_heading", "Rename a heading in a note and update every [[Note#Heading]] link to it, including #Parent#Child chains. Needs the note's etag.", destructive, renameHeading)
	addTool(s, "rename_block", "Rename a block ID (^id) in a note and update every [[Note#^id]] link to it. Needs the note's etag.", destructive, renameBlock)
	addTool(s, "rename_tag", "Rename a tag in every note, inline and in frontmatter, ignoring case. Nested tags follow (a → b renames a/x to b/x).", destructive, renameTag)
	addTool(s, "rename_property", "Rename a frontmatter property in every note that has it. Notes that already have the new name are skipped and listed.", destructive, renameProperty)
	addTool(s, "open_daily_note", "Open the daily note for a date (default today), creating it from the daily-note template if it doesn't exist. Returns its text and etag.", additive, openDailyNote)
	addTool(s, "append_to_daily_note", "Append text to the daily note for a date (default today), creating the note first if needed.", additive, appendToDailyNote)
	addTool(s, "create_from_template", "Create a note from a template in the template folder. {{title}}, {{date}} and {{time}} are filled in; other text, including Templater code, is copied as is.", additive, createFromTemplate)
	addTool(s, "attach_file", "Copy a local image, audio, video or PDF file into the vault's attachment folder (per the vault's settings, relative to note) and return the embed text to put in a note.", additive, attachFile)
	addTool(s, "replace_attachment", "Replace an existing image, audio, video or PDF file in the vault with a local file of the same type, keeping its path so every embed keeps working. Needs the file's etag from get_note_info. The old version can be restored with undo_last_write.", destructive, replaceAttachment)
	addTool(s, "undo_last_write", "Undo the newest change made through this server: one write, or a whole move or rename. Refused if the files changed since. Use dry_run to see what it would do.", destructive, undoLastWrite)
}

// --- change-set output

type moveOut struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type skipOut struct {
	Source string `json:"source"`
	Line   int    `json:"line,omitempty"`
	Link   string `json:"link"`
	Reason string `json:"reason"`
}

type changeSetOut struct {
	DryRun   bool      `json:"dry_run"`
	Files    []string  `json:"changed_files" jsonschema:"files whose content changes, at their new paths"`
	Moves    []moveOut `json:"moves,omitempty"`
	Rewrites int       `json:"links_rewritten"`
	Skipped  []skipOut `json:"skipped,omitempty"`
}

// applyPlan applies a plan's changes (or previews them) and reports them.
func applyPlan(ctx context.Context, vs *vaultState, what string, plan *links.Plan, report []string, skipped []skipOut, dry bool) (string, changeSetOut, error) {
	res, err := vs.v.Apply(ctx, plan.Changes, vault.WriteOptions{DryRun: dry})
	if err != nil {
		return "", changeSetOut{}, err
	}
	out := changeSetOut{DryRun: dry, Files: []string{}, Rewrites: len(plan.Rewrites), Skipped: skipped}
	for _, c := range plan.Changes {
		switch c.Kind {
		case vault.ChangeMove:
			out.Moves = append(out.Moves, moveOut{From: c.Path, To: c.To})
			if c.Data != nil {
				out.Files = append(out.Files, c.To)
			}
		default:
			out.Files = append(out.Files, c.Path)
		}
	}
	for _, r := range plan.Rewrites {
		report = append(report, fmt.Sprintf("%s:%d %s -> %s", r.Source, r.Line, r.Old, r.New))
	}
	for _, sk := range plan.Skipped {
		out.Skipped = append(out.Skipped, skipOut(sk))
	}
	for _, sk := range out.Skipped {
		report = append(report, fmt.Sprintf("skipped %s:%d %s: %s", sk.Source, sk.Line, sk.Link, sk.Reason))
	}
	o := newOut()
	verb := "Done"
	if dry {
		verb = "Dry run, nothing written"
	}
	o.line("%s: %s. %d files change, %d moves, %d links rewritten, %d skipped.", verb, what, len(out.Files), len(out.Moves), out.Rewrites, len(out.Skipped))
	if len(report) > 0 {
		r, _ := truncate(strings.Join(report, "\n"), listBytes)
		o.untrusted("report", r)
	}
	if dry {
		if d := res.Diff(); d != "" {
			d, cut := truncate(d, listBytes)
			if cut {
				d += "… (diff truncated)\n"
			}
			o.untrusted("diff", d)
		}
	}
	return o.String(), out, nil
}

// vaultPlan adapts a vault-wide rename plan.
func vaultPlan(p *vault.RenamePlan) (*links.Plan, []string, []skipOut) {
	var report []string
	var skipped []skipOut
	for _, f := range p.Files {
		report = append(report, f.Path+": "+f.Detail)
	}
	for _, f := range p.Skipped {
		skipped = append(skipped, skipOut{Source: f.Path, Link: "", Reason: f.Detail})
	}
	return &links.Plan{Changes: p.Changes}, report, skipped
}

// --- moves

type moveNoteIn struct {
	VaultArg
	Path string `json:"path" jsonschema:"the file to move"`
	To   string `json:"to" jsonschema:"its new vault-relative path, with the same extension"`
	DryRunArg
}

func moveNote(ctx context.Context, vs *vaultState, in moveNoteIn) (string, changeSetOut, error) {
	plan, err := vs.links.PlanMoves(ctx, map[string]string{in.Path: in.To})
	if err != nil {
		return "", changeSetOut{}, err
	}
	return applyPlan(ctx, vs, "move "+in.Path+" to "+in.To, plan, nil, nil, in.DryRun)
}

type renameNoteIn struct {
	VaultArg
	Path    string `json:"path" jsonschema:"the file to rename"`
	NewName string `json:"new_name" jsonschema:"the new file name, in the same folder; the extension may be omitted"`
	DryRunArg
}

func renameNote(ctx context.Context, vs *vaultState, in renameNoteIn) (string, changeSetOut, error) {
	name := strings.TrimSpace(in.NewName)
	if name == "" || strings.ContainsAny(name, "/\\") {
		return "", changeSetOut{}, errors.New("new_name must be a file name without folders; use move_note to change folders")
	}
	ext := path.Ext(in.Path)
	if !strings.EqualFold(path.Ext(name), ext) {
		name += ext
	}
	to := path.Join(path.Dir(in.Path), name)
	return moveNote(ctx, vs, moveNoteIn{Path: in.Path, To: to, DryRunArg: in.DryRunArg})
}

type moveFolderIn struct {
	VaultArg
	From string `json:"from" jsonschema:"the folder to move"`
	To   string `json:"to" jsonschema:"its new vault-relative path; must not exist"`
	DryRunArg
}

func moveFolder(ctx context.Context, vs *vaultState, in moveFolderIn) (string, changeSetOut, error) {
	plan, err := vs.links.PlanFolderMove(ctx, in.From, in.To)
	if err != nil {
		return "", changeSetOut{}, err
	}
	return applyPlan(ctx, vs, "move folder "+in.From+" to "+in.To, plan, nil, nil, in.DryRun)
}

type createFolderIn struct {
	VaultArg
	Path string `json:"path" jsonschema:"vault-relative folder path"`
	DryRunArg
}

func createFolder(ctx context.Context, vs *vaultState, in createFolderIn) (string, writeOut, error) {
	r, err := vs.v.CreateFolder(ctx, in.Path, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", writeOut{}, err
	}
	o := newOut()
	switch {
	case !r.Changed:
		o.line("The folder already exists.")
	case r.DryRun:
		o.line("Dry run: the folder would be created.")
	default:
		o.line("Created the folder.")
	}
	o.untrusted("path", r.Path)
	return o.String(), writeOut{Path: r.Path, Changed: r.Changed, DryRun: r.DryRun}, nil
}

// --- renames

type renameHeadingIn struct {
	VaultArg
	Path       string `json:"path" jsonschema:"the note"`
	ETag       string `json:"etag" jsonschema:"the etag from your latest read of the note"`
	Heading    string `json:"heading" jsonschema:"the heading to rename; use Parent#Heading to disambiguate"`
	NewHeading string `json:"new_heading" jsonschema:"the new heading text, without #s"`
	DryRunArg
}

func renameHeading(ctx context.Context, vs *vaultState, in renameHeadingIn) (string, changeSetOut, error) {
	plan, err := vs.links.PlanHeadingRename(ctx, in.Path, in.ETag, in.Heading, in.NewHeading)
	if err != nil {
		return "", changeSetOut{}, err
	}
	return applyPlan(ctx, vs, "rename heading "+in.Heading, plan, nil, nil, in.DryRun)
}

type renameBlockIn struct {
	VaultArg
	Path     string `json:"path" jsonschema:"the note"`
	ETag     string `json:"etag" jsonschema:"the etag from your latest read of the note"`
	Block    string `json:"block" jsonschema:"the block ID to rename, with or without ^"`
	NewBlock string `json:"new_block" jsonschema:"the new block ID: letters, digits and - only"`
	DryRunArg
}

func renameBlock(ctx context.Context, vs *vaultState, in renameBlockIn) (string, changeSetOut, error) {
	plan, err := vs.links.PlanBlockRename(ctx, in.Path, in.ETag, in.Block, in.NewBlock)
	if err != nil {
		return "", changeSetOut{}, err
	}
	return applyPlan(ctx, vs, "rename block ^"+strings.TrimPrefix(in.Block, "^"), plan, nil, nil, in.DryRun)
}

type renameTagIn struct {
	VaultArg
	Old string `json:"old" jsonschema:"the tag to rename, with or without #"`
	New string `json:"new" jsonschema:"the new tag name"`
	DryRunArg
}

func renameTag(ctx context.Context, vs *vaultState, in renameTagIn) (string, changeSetOut, error) {
	p, err := vs.v.PlanRenameTag(ctx, in.Old, in.New)
	if err != nil {
		return "", changeSetOut{}, err
	}
	plan, report, skipped := vaultPlan(p)
	return applyPlan(ctx, vs, "rename tag #"+strings.TrimPrefix(in.Old, "#"), plan, report, skipped, in.DryRun)
}

type renamePropertyIn struct {
	VaultArg
	Old string `json:"old" jsonschema:"the property to rename"`
	New string `json:"new" jsonschema:"the new property name"`
	DryRunArg
}

func renameProperty(ctx context.Context, vs *vaultState, in renamePropertyIn) (string, changeSetOut, error) {
	p, err := vs.v.PlanRenameProperty(ctx, in.Old, in.New)
	if err != nil {
		return "", changeSetOut{}, err
	}
	plan, report, skipped := vaultPlan(p)
	return applyPlan(ctx, vs, "rename property "+in.Old, plan, report, skipped, in.DryRun)
}

// --- daily notes and templates

type dailyIn struct {
	VaultArg
	Date string `json:"date,omitempty" jsonschema:"YYYY-MM-DD; default today"`
	DryRunArg
}

type dailyOut struct {
	Path    string `json:"path"`
	ETag    string `json:"etag"`
	Created bool   `json:"created"`
	DryRun  bool   `json:"dry_run"`
}

func parseDate(s string) (time.Time, error) {
	now := time.Now()
	if s == "" {
		return now, nil
	}
	d, err := time.ParseInLocation(time.DateOnly, s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("date %q is not YYYY-MM-DD", s)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), now.Hour(), now.Minute(), now.Second(), 0, time.Local), nil
}

func openDailyNote(ctx context.Context, vs *vaultState, in dailyIn) (string, dailyOut, error) {
	date, err := parseDate(in.Date)
	if err != nil {
		return "", dailyOut{}, err
	}
	n, err := daily.Open(ctx, vs.v, date, time.Now(), vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", dailyOut{}, err
	}
	res := dailyOut{Path: n.File.Path, ETag: n.File.ETag, Created: n.Created, DryRun: in.DryRun}
	o := newOut()
	switch {
	case n.Created && in.DryRun:
		o.line("Dry run: %s doesn't exist and would be created with this text.", n.File.Path)
	case n.Created:
		o.line("Created %s, etag %s", n.File.Path, n.File.ETag)
	default:
		o.line("%s, etag %s", n.File.Path, n.File.ETag)
	}
	text, cut := truncate(string(n.File.Data), noteBytes)
	if cut {
		o.line("Truncated; read the rest with read_note.")
	}
	o.untrusted(fmt.Sprintf("path=%q", n.File.Path), text)
	return o.String(), res, nil
}

type appendDailyIn struct {
	VaultArg
	Date string `json:"date,omitempty" jsonschema:"YYYY-MM-DD; default today"`
	Text string `json:"text" jsonschema:"text to add at the end of the note"`
	DryRunArg
}

func appendToDailyNote(ctx context.Context, vs *vaultState, in appendDailyIn) (string, writeOut, error) {
	date, err := parseDate(in.Date)
	if err != nil {
		return "", writeOut{}, err
	}
	n, err := daily.Open(ctx, vs.v, date, time.Now(), vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", writeOut{}, err
	}
	op := []vault.EditOp{{Kind: vault.EditAppend, Text: in.Text}}
	if n.Created && in.DryRun {
		data, err := vault.ApplyEdits(n.File.Data, op)
		if err != nil {
			return "", writeOut{}, err
		}
		return writeResult("created and appended to", &vault.Result{Path: n.File.Path, ETag: vault.ETag(data), DryRun: true, Changed: true, Diff: fmt.Sprintf("create %s\n%s", n.File.Path, string(data))})
	}
	r, err := vs.v.Edit(ctx, n.File.Path, n.File.ETag, op, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", writeOut{}, err
	}
	return writeResult("appended to", r)
}

type listTemplatesIn struct{ VaultArg }

func listTemplates(ctx context.Context, vs *vaultState, _ listTemplatesIn) (string, pathsOut, error) {
	all, err := daily.ListTemplates(ctx, vs.v)
	if err != nil {
		return "", pathsOut{}, err
	}
	o := newOut()
	o.line("%d templates.", len(all))
	if len(all) > 0 {
		t, _ := truncate(strings.Join(all, "\n"), listBytes)
		o.untrusted("paths", t)
	}
	return o.String(), pathsOut{Paths: nonNil(all), Total: len(all)}, nil
}

type fromTemplateIn struct {
	VaultArg
	Template string `json:"template" jsonschema:"a template from list_templates, by path or by name in the template folder"`
	Path     string `json:"path" jsonschema:"the new note's vault-relative path, ending in .md"`
	DryRunArg
}

func createFromTemplate(ctx context.Context, vs *vaultState, in fromTemplateIn) (string, writeOut, error) {
	if err := noteOnly("create", in.Path); err != nil {
		return "", writeOut{}, err
	}
	r, err := daily.CreateFromTemplate(ctx, vs.v, in.Template, in.Path, time.Now(), vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", writeOut{}, err
	}
	return writeResult("created", r)
}

// --- attachments

type attachIn struct {
	VaultArg
	File string `json:"file" jsonschema:"absolute path of a local image, audio, video or PDF file"`
	Note string `json:"note,omitempty" jsonschema:"the note the file is for; sets the folder when attachments go next to notes"`
	Name string `json:"name,omitempty" jsonschema:"file name in the vault; default: the file's own name"`
	DryRunArg
}

type attachOut struct {
	Path   string `json:"path"`
	Embed  string `json:"embed" jsonschema:"text to put in the note to embed the file"`
	DryRun bool   `json:"dry_run"`
}

func attachFile(ctx context.Context, vs *vaultState, in attachIn) (string, attachOut, error) {
	data, err := readLocalAttachment(in.File)
	if err != nil {
		return "", attachOut{}, err
	}
	name := in.Name
	if name == "" {
		name = filepath.Base(in.File)
	}
	if strings.ContainsAny(name, "/\\") {
		return "", attachOut{}, errors.New("name must be a file name without folders")
	}
	cfg, err := vs.v.Config(ctx)
	if err != nil {
		return "", attachOut{}, err
	}
	note := ""
	if in.Note != "" {
		if note, err = vault.Clean(in.Note); err != nil {
			return "", attachOut{}, err
		}
	}
	dst := path.Join(attachmentFolder(cfg.AttachmentFolderPath, note), name)
	r, err := vs.v.CreateAttachment(ctx, dst, data, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", attachOut{}, err
	}
	style := links.Wikilink
	if cfg.UseMarkdownLinks {
		style = links.Markdown
	}
	t, err := vs.links.LinkTo(ctx, note, r.Path, style)
	if err != nil {
		return "", attachOut{}, err
	}
	embed := "![[" + t + "]]"
	if style == links.Markdown {
		embed = "![](" + t + ")"
	}
	o := newOut()
	if r.DryRun {
		o.line("Dry run: the file would be copied (%d bytes). Nothing was written.", len(data))
	} else {
		o.line("Copied the file (%d bytes). Put the embed text in the note.", len(data))
	}
	o.untrusted("attachment", "path: "+r.Path+"\nembed: "+embed)
	return o.String(), attachOut{Path: r.Path, Embed: embed, DryRun: r.DryRun}, nil
}

type replaceAttachmentIn struct {
	VaultArg
	Path string `json:"path" jsonschema:"the attachment in the vault to replace, such as Media/clip.mp4"`
	ETag string `json:"etag" jsonschema:"the file's etag from get_note_info"`
	File string `json:"file" jsonschema:"absolute path of the local file with the new content; it must have the same extension"`
	DryRunArg
}

type replaceAttachmentOut struct {
	writeOut
	Size int `json:"size" jsonschema:"the new file's size in bytes"`
}

func replaceAttachment(ctx context.Context, vs *vaultState, in replaceAttachmentIn) (string, replaceAttachmentOut, error) {
	if !strings.EqualFold(path.Ext(in.Path), filepath.Ext(in.File)) {
		return "", replaceAttachmentOut{}, fmt.Errorf("the new file's extension %q differs from %s; a new format means a new file name, so use attach_file for it and update the embeds that should show it", filepath.Ext(in.File), in.Path)
	}
	data, err := readLocalAttachment(in.File)
	if err != nil {
		return "", replaceAttachmentOut{}, err
	}
	r, err := vs.v.ReplaceAttachment(ctx, in.Path, data, in.ETag, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", replaceAttachmentOut{}, err
	}
	text, w, err := writeResult("replaced", r)
	if err != nil {
		return "", replaceAttachmentOut{}, err
	}
	return text, replaceAttachmentOut{writeOut: w, Size: len(data)}, nil
}

// attachmentFolder is where a new attachment for note goes (V-ATT-01): "/"
// or "" is the vault root, "./" the note's folder, "./sub" a subfolder of
// it, and anything else that vault folder.
func attachmentFolder(setting, note string) string {
	dir := path.Dir(note)
	if note == "" || dir == "." {
		dir = ""
	}
	switch s := strings.TrimSpace(setting); {
	case s == "" || s == "/":
		return ""
	case s == "./" || s == ".":
		return dir
	case strings.HasPrefix(s, "./"):
		return path.Join(dir, strings.Trim(s[2:], "/"))
	default:
		return strings.Trim(s, "/")
	}
}

// readLocalAttachment reads a local file for attach_file. It must be an
// absolute path to a regular file (no symlink, no dot-folder on the way,
// such as ~/.ssh) of an attachment kind, at most maxAttachment bytes
// [policy D-012].
func readLocalAttachment(p string) ([]byte, error) {
	if !filepath.IsAbs(p) {
		return nil, errors.New("file must be an absolute path")
	}
	clean := filepath.Clean(p)
	for seg := range strings.SplitSeq(filepath.ToSlash(clean), "/") {
		if strings.HasPrefix(seg, ".") {
			return nil, fmt.Errorf("refusing %s: files in hidden folders can't be attached", p)
		}
	}
	switch vault.KindOf(clean) {
	case vault.KindImage, vault.KindAudio, vault.KindVideo, vault.KindPDF:
	default:
		return nil, fmt.Errorf("refusing %s: only images, audio, video and PDF can be attached", p)
	}
	fi, err := os.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing %s: not a regular file", p)
	}
	if fi.Size() > maxAttachment {
		return nil, fmt.Errorf("refusing %s: larger than %d MB", p, maxAttachment>>20)
	}
	return os.ReadFile(clean) //nolint:gosec // the path is checked above; reading it is the tool's job
}

// --- undo

type undoIn struct {
	VaultArg
	DryRunArg
}

type undoOut struct {
	DryRun  bool     `json:"dry_run"`
	Undone  []string `json:"undone" jsonschema:"the changes reversed, newest first"`
	Changed []string `json:"changed_files"`
}

func undoLastWrite(ctx context.Context, vs *vaultState, in undoIn) (string, undoOut, error) {
	r, err := vs.v.Undo(ctx, vault.WriteOptions{DryRun: in.DryRun})
	switch {
	case errors.Is(err, vault.ErrNoJournal):
		return "", undoOut{}, fmt.Errorf("%w. Hint: start the server with the journal on (OBS_MCP_JOURNAL=on)", err)
	case errors.Is(err, vault.ErrNothingToUndo):
		return "", undoOut{}, err
	case err != nil:
		return "", undoOut{}, err
	}
	res := undoOut{DryRun: in.DryRun, Undone: []string{}, Changed: []string{}}
	for _, e := range r.Entries {
		desc := e.Op + " " + e.Path
		if e.NewPath != "" {
			desc += " → " + e.NewPath
		}
		res.Undone = append(res.Undone, desc)
	}
	for _, x := range r.Results {
		res.Changed = append(res.Changed, x.Path)
	}
	o := newOut()
	if in.DryRun {
		o.line("Dry run: undo would reverse %d changes. Nothing was written.", len(r.Entries))
	} else {
		o.line("Undid %d changes.", len(r.Entries))
	}
	d, cut := truncate(strings.Join(res.Undone, "\n")+"\n"+r.Diff(), listBytes)
	if cut {
		d += "… (truncated)\n"
	}
	o.untrusted("undo", d)
	return o.String(), res, nil
}
