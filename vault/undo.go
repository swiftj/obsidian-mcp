package vault

import (
	"context"
	"errors"
	"fmt"
	"path"
)

// ErrNothingToUndo is returned by Undo when the journal has no entry for
// the vault.
var ErrNothingToUndo = errors.New("vault: nothing to undo")

// ErrNoJournal is returned by Undo when journaling is off.
var ErrNoJournal = errors.New("vault: undo needs the journal, which is off")

// UndoResult reports an undo.
type UndoResult struct {
	*BatchResult
	Entries []JournalEntry // the entries undone, newest first
}

// Undo reverses the newest journaled change of the vault: one write, or a
// whole change set (V-JRN-03). Each file must still be as that change left
// it, or the undo is refused with *ConflictError. An undone create moves the
// file to .trash rather than deleting it. Undo is not journaled itself; its
// entries are removed, so the next Undo goes one change further back.
func (v *Vault) Undo(ctx context.Context, o WriteOptions) (*UndoResult, error) {
	if v.journal == nil {
		return nil, ErrNoJournal
	}
	all, err := v.journal.Entries(v)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, ErrNothingToUndo
	}
	todo := all[:1]
	if b := all[0].Batch; b != "" {
		n := 1
		for n < len(all) && all[n].Batch == b {
			n++
		}
		todo = all[:n]
		if n != all[0].BatchSize {
			return nil, fmt.Errorf("vault: the newest change set was partly pruned from the journal (%d of %d entries left) and can't be undone", n, all[0].BatchSize)
		}
	}
	var changes []Change
	for _, e := range todo { // newest first, the order to reverse them in
		if e.ETagAfter == "" {
			return nil, fmt.Errorf("vault: journal entry %s predates undo support and can't be undone", e.ID)
		}
		c, err := v.inverse(e)
		if err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	res, err := v.apply(ctx, changes, o, applyOpts{allowTrash: true, allowAttachments: true})
	if err != nil {
		return nil, err
	}
	if !o.DryRun {
		for _, e := range todo {
			v.journal.drop(path.Join(vaultKey(v), e.ID))
		}
	}
	return &UndoResult{BatchResult: res, Entries: todo}, nil
}

// inverse returns the change that reverses journal entry e.
func (v *Vault) inverse(e JournalEntry) (Change, error) {
	switch e.Op {
	case "create":
		dst, err := v.freeTrashPath(e.Path)
		if err != nil {
			return Change{}, err
		}
		return Change{Kind: ChangeMove, Path: e.Path, To: dst, ETag: e.ETagAfter}, nil
	case "replace":
		data, err := v.journal.Data(v, e.ID)
		if err != nil {
			return Change{}, err
		}
		return Change{Kind: ChangeWrite, Path: e.Path, ETag: e.ETagAfter, Data: data}, nil
	case "trash", "restore", "move":
		c := Change{Kind: ChangeMove, Path: e.NewPath, To: e.Path, ETag: e.ETagAfter}
		if e.HasData {
			data, err := v.journal.Data(v, e.ID)
			if err != nil {
				return Change{}, err
			}
			c.Data = data
		}
		return c, nil
	}
	return Change{}, fmt.Errorf("vault: journal entry %s has unknown op %q", e.ID, e.Op)
}
