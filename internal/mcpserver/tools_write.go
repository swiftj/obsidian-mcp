package mcpserver

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/swiftj/obsidian-mcp/vault"
)

func (s *Server) addWriteTools() {
	addTool(s, "create_note", "Create a new note (.md). Fails if the file exists. Missing folders are created.", additive, createNote)
	addTool(s, "edit_note", "Edit a note in place with small patches; everything outside the edited ranges stays byte for byte. Ops apply in order: replace_section (heading), insert_after_heading (heading), append, prepend (after the frontmatter), replace_text (find, count = the exact number of matches), replace_block (block ^id). Needs the etag from read_note.", destructive, editNote)
	addTool(s, "replace_note", "Replace a note's whole text. Prefer edit_note for partial changes. Needs the etag from read_note.", destructive, replaceNote)
	addTool(s, "set_properties", "Set frontmatter properties of a note, checked against the vault's property types. Only the changed properties' lines are rewritten. Needs the etag.", destructive, setProperties)
	addTool(s, "remove_properties", "Remove frontmatter properties from a note. Needs the etag.", destructive, removeProperties)
	addTool(s, "delete_note", "Move a note or other file to the vault's .trash folder. restore_from_trash undoes it. Needs the etag.", destructive, deleteNote)
	addTool(s, "restore_from_trash", "Move a file from .trash back to where it was, or to another path. Fails if the destination exists.", additive, restoreFromTrash)
}

// DryRunArg is the dry_run argument every write tool takes (X-SRV-05).
type DryRunArg struct {
	DryRun bool `json:"dry_run,omitempty" jsonschema:"preview: return the diff and change nothing"`
}

type writeOut struct {
	Path    string `json:"path"`
	ETag    string `json:"etag,omitempty" jsonschema:"the file's etag after the write; use it for the next edit"`
	Changed bool   `json:"changed"`
	DryRun  bool   `json:"dry_run"`
}

// writeResult reports a write: a summary line and the diff, which holds
// note text and so is wrapped.
func writeResult(verb string, r *vault.Result) (string, writeOut, error) {
	o := newOut()
	switch {
	case r.DryRun:
		o.line("Dry run: %s would be %s. Nothing was written.", r.Path, verb)
	case !r.Changed:
		o.line("%s is unchanged.", r.Path)
	default:
		o.line("%s %s. New etag %s", r.Path, verb, r.ETag)
	}
	if r.Diff != "" {
		d, cut := truncate(r.Diff, listBytes)
		if cut {
			d += "… (diff truncated)\n"
		}
		o.untrusted(fmt.Sprintf("diff path=%q", r.Path), d)
	}
	return o.String(), writeOut{Path: r.Path, ETag: r.ETag, Changed: r.Changed, DryRun: r.DryRun}, nil
}

// noteOnly refuses paths that aren't notes: canvases and bases get their own
// tools [policy D-011].
func noteOnly(op, p string) error {
	if strings.ToLower(path.Ext(p)) != ".md" {
		return &vault.PathError{Op: op, Path: p, Reason: "this tool writes notes (.md) only"}
	}
	return nil
}

type createNoteIn struct {
	VaultArg
	Path    string `json:"path" jsonschema:"vault-relative path ending in .md"`
	Content string `json:"content" jsonschema:"the note's full text, including any frontmatter"`
	DryRunArg
}

func createNote(ctx context.Context, vs *vaultState, in createNoteIn) (string, writeOut, error) {
	if err := noteOnly("create", in.Path); err != nil {
		return "", writeOut{}, err
	}
	r, err := vs.v.Create(ctx, in.Path, []byte(in.Content), vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", writeOut{}, err
	}
	return writeResult("created", r)
}

type editOpIn struct {
	Op      string `json:"op" jsonschema:"replace_section, insert_after_heading, append, prepend, replace_text or replace_block"`
	Heading string `json:"heading,omitempty" jsonschema:"replace_section and insert_after_heading: the heading text, or Parent#Heading"`
	Block   string `json:"block,omitempty" jsonschema:"replace_block: the block ID, with or without ^"`
	Find    string `json:"find,omitempty" jsonschema:"replace_text: the exact text to find"`
	Count   int    `json:"count,omitempty" jsonschema:"replace_text: how many times find appears; every match is replaced"`
	Text    string `json:"text" jsonschema:"the new text"`
}

type editNoteIn struct {
	VaultArg
	Path string     `json:"path" jsonschema:"vault-relative path ending in .md"`
	ETag string     `json:"etag" jsonschema:"the etag from your latest read of the note"`
	Ops  []editOpIn `json:"ops" jsonschema:"edits, applied in order"`
	DryRunArg
}

var editKinds = map[string]vault.EditKind{
	"replace_section":      vault.EditReplaceSection,
	"insert_after_heading": vault.EditInsertAfterHeading,
	"append":               vault.EditAppend,
	"prepend":              vault.EditPrepend,
	"replace_text":         vault.EditReplaceText,
	"replace_block":        vault.EditReplaceBlock,
}

func editNote(ctx context.Context, vs *vaultState, in editNoteIn) (string, writeOut, error) {
	if err := noteOnly("edit", in.Path); err != nil {
		return "", writeOut{}, err
	}
	if len(in.Ops) == 0 {
		return "", writeOut{}, fmt.Errorf("ops is empty")
	}
	ops := make([]vault.EditOp, 0, len(in.Ops))
	for i, op := range in.Ops {
		k, ok := editKinds[op.Op]
		if !ok {
			return "", writeOut{}, fmt.Errorf("ops[%d]: unknown op %q; use replace_section, insert_after_heading, append, prepend, replace_text or replace_block", i, op.Op)
		}
		ops = append(ops, vault.EditOp{Kind: k, Heading: op.Heading, Block: op.Block, Find: op.Find, Count: op.Count, Text: op.Text})
	}
	r, err := vs.v.Edit(ctx, in.Path, in.ETag, ops, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", writeOut{}, err
	}
	return writeResult("edited", r)
}

type replaceNoteIn struct {
	VaultArg
	Path    string `json:"path" jsonschema:"vault-relative path ending in .md"`
	ETag    string `json:"etag" jsonschema:"the etag from your latest read of the note"`
	Content string `json:"content" jsonschema:"the note's new full text; line endings are converted to the note's style"`
	DryRunArg
}

func replaceNote(ctx context.Context, vs *vaultState, in replaceNoteIn) (string, writeOut, error) {
	if err := noteOnly("replace", in.Path); err != nil {
		return "", writeOut{}, err
	}
	r, err := vs.v.Replace(ctx, in.Path, []byte(in.Content), in.ETag, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", writeOut{}, err
	}
	return writeResult("replaced", r)
}

type setPropertiesIn struct {
	VaultArg
	Path       string         `json:"path" jsonschema:"vault-relative path ending in .md"`
	ETag       string         `json:"etag" jsonschema:"the etag from your latest read of the note"`
	Properties map[string]any `json:"properties" jsonschema:"property name to value: a string, number, boolean, date string (YYYY-MM-DD), or list; links must be written as \"[[Note]]\""`
	DryRunArg
}

func setProperties(ctx context.Context, vs *vaultState, in setPropertiesIn) (string, writeOut, error) {
	if len(in.Properties) == 0 {
		return "", writeOut{}, fmt.Errorf("properties is empty")
	}
	r, err := vs.v.SetProperties(ctx, in.Path, in.ETag, in.Properties, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", writeOut{}, err
	}
	return writeResult("updated", r)
}

type removePropertiesIn struct {
	VaultArg
	Path  string   `json:"path" jsonschema:"vault-relative path ending in .md"`
	ETag  string   `json:"etag" jsonschema:"the etag from your latest read of the note"`
	Names []string `json:"names" jsonschema:"property names to remove; absent names are ignored"`
	DryRunArg
}

func removeProperties(ctx context.Context, vs *vaultState, in removePropertiesIn) (string, writeOut, error) {
	if len(in.Names) == 0 {
		return "", writeOut{}, fmt.Errorf("names is empty")
	}
	r, err := vs.v.RemoveProperties(ctx, in.Path, in.ETag, in.Names, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", writeOut{}, err
	}
	return writeResult("updated", r)
}

type deleteNoteIn struct {
	VaultArg
	Path string `json:"path" jsonschema:"vault-relative path"`
	ETag string `json:"etag" jsonschema:"the etag from your latest read of the file (read_note or get_note_info)"`
	DryRunArg
}

type deleteOut struct {
	TrashPath string `json:"trash_path" jsonschema:"pass this to restore_from_trash to undo"`
	Changed   bool   `json:"changed"`
	DryRun    bool   `json:"dry_run"`
}

func deleteNote(ctx context.Context, vs *vaultState, in deleteNoteIn) (string, deleteOut, error) {
	r, err := vs.v.Trash(ctx, in.Path, in.ETag, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", deleteOut{}, err
	}
	o := newOut()
	if r.DryRun {
		o.line("Dry run: %s would be moved to the trash. Nothing was changed.", in.Path)
	} else {
		o.line("Moved %s to the trash; restore_from_trash with trash_path below undoes it.", in.Path)
	}
	// Restore and list_trash use paths inside .trash.
	tp := strings.TrimPrefix(r.Path, ".trash/")
	o.untrusted("trash_path", tp)
	return o.String(), deleteOut{TrashPath: tp, Changed: r.Changed, DryRun: r.DryRun}, nil
}

type restoreIn struct {
	VaultArg
	TrashPath string `json:"trash_path" jsonschema:"the trash_path from list_trash or delete_note"`
	To        string `json:"to,omitempty" jsonschema:"where to put it; default: where it was deleted from"`
	DryRunArg
}

func restoreFromTrash(ctx context.Context, vs *vaultState, in restoreIn) (string, writeOut, error) {
	r, err := vs.v.Restore(ctx, in.TrashPath, in.To, vault.WriteOptions{DryRun: in.DryRun})
	if err != nil {
		return "", writeOut{}, err
	}
	o := newOut()
	if r.DryRun {
		o.line("Dry run: the file would be restored. Nothing was changed.")
	} else {
		o.line("Restored, etag %s.", r.ETag)
	}
	o.untrusted("path", r.Path)
	return o.String(), writeOut{Path: r.Path, ETag: r.ETag, Changed: r.Changed, DryRun: r.DryRun}, nil
}
