package vault

import (
	"context"
	"errors"
	"fmt"

	"github.com/swiftj/obsidian-mcp/vault/ofm"
	"github.com/swiftj/obsidian-mcp/vault/props"
)

// RenamePlan is a planned vault-wide rename: the changes for Apply, what
// each changed file gets, and the files left alone.
type RenamePlan struct {
	Changes []Change
	Files   []PlanFile // changed files, by path
	Skipped []PlanFile // files that need the rename but can't take it
}

// PlanFile is one file of a RenamePlan.
type PlanFile struct {
	Path   string
	Detail string
}

// PlanRenameTag plans renaming tag old to new in every visible note
// (P-TAGREN-01), inline and in frontmatter, nested tags included.
func (v *Vault) PlanRenameTag(ctx context.Context, old, new string) (*RenamePlan, error) {
	if !props.ValidTag(ofm.NormTag(new)) && !props.ValidTag(new) {
		return nil, &props.Error{Name: "tags", Reason: fmt.Sprintf("%q is not a valid tag", new), Hint: "tags are letters, digits, _, - and / with at least one non-digit and no spaces"}
	}
	return v.planEachNote(ctx, func(src []byte) ([]byte, string, error) {
		out, n, err := props.RenameTag(src, old, new)
		if err != nil || n == 0 {
			return nil, "", err
		}
		return out, fmt.Sprintf("%d tags", n), nil
	})
}

// PlanRenameProperty plans renaming property old to new in every visible
// note that has it (P-REN-01). A note that already has new, or whose
// frontmatter can't be patched, is skipped and reported.
func (v *Vault) PlanRenameProperty(ctx context.Context, old, new string) (*RenamePlan, error) {
	canon := props.Canonical(old)
	return v.planEachNote(ctx, func(src []byte) ([]byte, string, error) {
		f := props.Read(ofm.Parse(src))
		if f.Err != nil {
			return nil, "", &props.Error{Name: canon, Reason: "the frontmatter can't be read, so it may hold the property", Hint: "fix the frontmatter by hand"}
		}
		if f.Get(canon) == nil {
			return nil, "", nil
		}
		out, err := props.Rename(src, old, new)
		if err != nil {
			return nil, "", err
		}
		return out, "renamed", nil
	})
}

// planEachNote runs change on every visible note. change returns the new
// bytes, or nil for a note it leaves alone; a *props.Error skips the note.
func (v *Vault) planEachNote(ctx context.Context, change func([]byte) ([]byte, string, error)) (*RenamePlan, error) {
	plan := &RenamePlan{}
	for e, err := range v.List(ctx, "", true) {
		if err != nil {
			return nil, err
		}
		if e.Kind != KindMarkdown {
			continue
		}
		f, err := v.Read(ctx, e.Path)
		if err != nil {
			return nil, err
		}
		out, detail, err := change(f.Data)
		var pe *props.Error
		switch {
		case errors.As(err, &pe):
			plan.Skipped = append(plan.Skipped, PlanFile{Path: f.Path, Detail: pe.Error()})
		case err != nil:
			return nil, err
		case out != nil && string(out) != string(f.Data):
			plan.Changes = append(plan.Changes, Change{Kind: ChangeWrite, Path: f.Path, ETag: f.ETag, Data: out})
			plan.Files = append(plan.Files, PlanFile{Path: f.Path, Detail: detail})
		}
	}
	return plan, nil
}
