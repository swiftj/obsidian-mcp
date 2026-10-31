package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/swiftj/obsidian-mcp/internal/udiff"
)

// ChangeKind is what a Change does.
type ChangeKind string

const (
	ChangeWrite  ChangeKind = "write"  // replace an existing file's bytes
	ChangeCreate ChangeKind = "create" // make a new file
	ChangeMove   ChangeKind = "move"   // move a file, optionally with new bytes
)

// Change is one step of a change set.
type Change struct {
	Kind ChangeKind
	Path string // the file; for a move, its current path
	To   string // ChangeMove: the new path
	// ETag is the file's current etag (ChangeWrite, ChangeMove).
	ETag string
	// Data is the new content. For a move, nil keeps the file's bytes.
	Data []byte
}

// BatchResult reports a change set.
type BatchResult struct {
	Results []Result // one per change, in order
	DryRun  bool
}

// Diff joins the diffs of every change.
func (b *BatchResult) Diff() string {
	var sb strings.Builder
	for _, r := range b.Results {
		sb.WriteString(r.Diff)
	}
	return sb.String()
}

// RollbackError reports a change set that failed part-way and could not be
// fully reversed. Err is the original failure.
type RollbackError struct {
	Err      error
	Rollback error
}

func (e *RollbackError) Error() string {
	return fmt.Sprintf("vault: change set failed (%v) and rolling back failed too (%v); check the files listed", e.Err, e.Rollback)
}

func (e *RollbackError) Unwrap() error { return e.Err }

// Apply makes every change or none (L-REN-06). Under the write lock, every
// etag and destination is checked before anything is written; a conflict
// refuses the whole set. If a step then fails, the finished steps are
// reversed. Every step is journaled under one batch ID, so Undo reverses the
// set as a whole. A path may appear in only one change.
func (v *Vault) Apply(ctx context.Context, changes []Change, o WriteOptions) (*BatchResult, error) {
	return v.apply(ctx, changes, o, applyOpts{journal: true})
}

type applyOpts struct {
	journal    bool // record the steps for undo
	allowTrash bool // undo may move files into and out of .trash
	// allowAttachments lets undo write back the journaled bytes of an
	// image, audio, video or PDF file. No tool sets it (D-015).
	allowAttachments bool
}

// binary reports whether a change may write non-text bytes to p: only undo,
// and only for attachment kinds.
func (ao applyOpts) binary(p string) bool {
	return ao.allowAttachments && attachmentKinds[KindOf(p)]
}

// step is a planned, checked change.
type step struct {
	c      Change
	from   string // clean source path (the file for write and create)
	to     string // clean destination for a move
	before []byte // current bytes; nil for create
	after  []byte // bytes after the step
	perm   fs.FileMode
	moved  bool // set once the move is done
	wrote  bool // set once the bytes are written
}

func (v *Vault) apply(ctx context.Context, changes []Change, o WriteOptions, ao applyOpts) (*BatchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	steps, err := v.planSteps(changes, ao)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	res := &BatchResult{DryRun: o.DryRun}
	for _, s := range steps {
		if err := v.checkStep(s); err != nil {
			return nil, err
		}
		r := Result{Path: s.from, ETag: ETag(s.after), DryRun: o.DryRun, Changed: true}
		switch s.c.Kind {
		case ChangeMove:
			r.Path = s.to
			r.Diff = fmt.Sprintf("rename %s → %s\n", s.from, s.to)
			if !bytes.Equal(s.before, s.after) {
				r.Diff += udiff.Unified(s.to, s.before, s.after)
			}
		default:
			r.Diff = udiff.Unified(s.from, s.before, s.after)
			r.Changed = s.c.Kind == ChangeCreate || !bytes.Equal(s.before, s.after)
		}
		res.Results = append(res.Results, r)
	}
	if o.DryRun {
		return res, nil
	}
	batch := fmt.Sprintf("b%d", time.Now().UnixNano())
	var jdirs, newDirs []string
	fail := func(i int, err error) (*BatchResult, error) {
		rerr := v.rollback(steps[:i+1], newDirs)
		for _, d := range jdirs {
			v.journalDrop(d)
		}
		if rerr != nil {
			return nil, &RollbackError{Err: err, Rollback: rerr}
		}
		return nil, err
	}
	for i, s := range steps {
		if v.testFail != nil {
			if err := v.testFail(i); err != nil {
				return fail(i, err)
			}
		}
		if ao.journal {
			e := JournalEntry{Batch: batch, BatchSize: len(steps), Path: s.from, ETagBefore: s.c.ETag, ETagAfter: ETag(s.after)}
			var keep []byte
			switch s.c.Kind {
			case ChangeWrite:
				e.Op, keep = "replace", s.before
			case ChangeCreate:
				e.Op = "create"
			case ChangeMove:
				e.Op, e.NewPath = "move", s.to
				if !bytes.Equal(s.before, s.after) {
					keep = s.before
				}
			}
			d, err := v.journalRecord(e, keep)
			if err != nil {
				return fail(i, err)
			}
			if d != "" {
				jdirs = append(jdirs, d)
			}
		}
		if err := v.runStep(steps[i], &newDirs); err != nil {
			return fail(i, err)
		}
	}
	v.removeEmptied(steps)
	return res, nil
}

// removeEmptied removes the folders that the set's moves left empty, as
// Obsidian does when a folder is moved. Folders that still hold anything,
// hidden files included, stay.
func (v *Vault) removeEmptied(steps []*step) {
	for _, s := range steps {
		if s.c.Kind != ChangeMove {
			continue
		}
		for d := path.Dir(s.from); d != "." && d != "" && d != trashDir; d = path.Dir(d) {
			if v.root.Remove(d) != nil { // fails unless empty
				break
			}
		}
	}
}

// planSteps checks paths and kinds without touching the disk.
func (v *Vault) planSteps(changes []Change, ao applyOpts) ([]*step, error) {
	seen := map[string]bool{}
	claim := func(p string) error {
		k := strings.ToLower(p)
		if seen[k] {
			return &PathError{Op: "apply", Path: p, Reason: "appears in more than one change"}
		}
		seen[k] = true
		return nil
	}
	steps := make([]*step, 0, len(changes))
	for _, c := range changes {
		s := &step{c: c}
		var err error
		switch c.Kind {
		case ChangeWrite, ChangeCreate:
			a := accessWrite
			if c.Kind == ChangeWrite && ao.binary(c.Path) {
				a = accessFile
			}
			if s.from, err = v.resolve(string(c.Kind), c.Path, a); err != nil {
				return nil, err
			}
			if c.Kind == ChangeWrite && c.ETag == "" {
				return nil, ErrETagRequired
			}
			if c.Kind == ChangeCreate {
				if reason := badName(path.Base(s.from)); reason != "" {
					return nil, &PathError{Op: "create", Path: s.from, Reason: reason}
				}
			}
			if a == accessWrite {
				if err := checkText(string(c.Kind), s.from, c.Data); err != nil {
					return nil, err
				}
			}
		case ChangeMove:
			if s.from, err = v.movePath(c.Path, ao.allowTrash); err != nil {
				return nil, err
			}
			if s.to, err = v.movePath(c.To, ao.allowTrash); err != nil {
				return nil, err
			}
			if c.ETag == "" {
				return nil, ErrETagRequired
			}
			if !strings.EqualFold(path.Ext(s.from), path.Ext(s.to)) {
				return nil, &PathError{Op: "move", Path: s.to, Reason: "a move can't change the file's extension"}
			}
			if reason := badName(path.Base(s.to)); reason != "" && !strings.HasPrefix(s.to, trashDir+"/") {
				return nil, &PathError{Op: "move", Path: s.to, Reason: reason}
			}
			if c.Data != nil && !ao.binary(s.to) {
				if !writableExt(s.to) {
					return nil, &PathError{Op: "move", Path: s.to, Reason: "only .md, .canvas and .base files can be rewritten"}
				}
				if err := checkText("move", s.to, c.Data); err != nil {
					return nil, err
				}
			}
			if err := claim(s.to); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("vault: unknown change kind %q", c.Kind)
		}
		if err := claim(s.from); err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	return steps, nil
}

// movePath cleans a move endpoint: any visible file kind, or a path under
// .trash when undo allows it.
func (v *Vault) movePath(p string, allowTrash bool) (string, error) {
	rel, err := Clean(p)
	if err != nil {
		err.(*PathError).Op = "move"
		return "", err
	}
	if allowTrash && strings.HasPrefix(rel, trashDir+"/") {
		if reason := v.pol.hiddenReason(strings.TrimPrefix(rel, trashDir+"/")); reason != "" {
			return "", &PathError{Op: "move", Path: rel, Reason: reason}
		}
		return rel, nil
	}
	if err := v.pol.check("move", rel, accessRead); err != nil {
		return "", err
	}
	if rel == "" {
		return "", &PathError{Op: "move", Path: rel, Reason: "the vault root is not a file"}
	}
	return rel, nil
}

// checkStep reads the current state for a step and checks its etag and
// destination. The caller holds v.mu.
func (v *Vault) checkStep(s *step) error {
	switch s.c.Kind {
	case ChangeCreate:
		if cur, _, err := v.readRegular("create", s.from); err == nil {
			return &ConflictError{Path: s.from, Current: ETag(cur)}
		} else if !isNotFound(err) {
			return err
		}
		s.after, s.perm = s.c.Data, 0o644
		return nil
	}
	cur, fi, err := v.readRegular(string(s.c.Kind), s.from)
	if err != nil {
		return err
	}
	if t := ETag(cur); t != s.c.ETag {
		return &ConflictError{Path: s.from, Expected: s.c.ETag, Current: t}
	}
	s.before, s.after, s.perm = cur, s.c.Data, fi.Mode().Perm()
	if s.c.Kind == ChangeMove {
		if s.after == nil {
			s.after = cur
		}
		if now, _, err := v.readRegular("move", s.to); err == nil {
			return &ConflictError{Path: s.to, Current: ETag(now)}
		} else if !isNotFound(err) {
			return err
		}
	}
	return nil
}

// runStep performs one checked step. The caller holds v.mu.
func (v *Vault) runStep(s *step, newDirs *[]string) error {
	switch s.c.Kind {
	case ChangeWrite:
		err := v.replaceAtomic(s.from, s.after, s.perm, func() error {
			now, _, err := v.readRegular("write", s.from)
			if err != nil {
				return err
			}
			if t := ETag(now); t != s.c.ETag {
				return &ConflictError{Path: s.from, Expected: s.c.ETag, Current: t}
			}
			return nil
		})
		s.wrote = err == nil
		return err
	case ChangeCreate:
		if err := v.mkdirsTracked("create", s.from, newDirs); err != nil {
			return err
		}
		if err := v.createAtomic(s.from, s.after, s.perm); err != nil {
			if errors.Is(err, fs.ErrExist) {
				cur, _, _ := v.readRegular("create", s.from)
				return &ConflictError{Path: s.from, Current: ETag(cur)}
			}
			return err
		}
		s.wrote = true
		return nil
	}
	if err := v.mkdirsTracked("move", s.to, newDirs); err != nil {
		return err
	}
	if err := v.moveNoReplace(s.from, s.to); err != nil {
		if errors.Is(err, fs.ErrExist) {
			cur, _, _ := v.readRegular("move", s.to)
			return &ConflictError{Path: s.to, Current: ETag(cur)}
		}
		return err
	}
	s.moved = true
	if !bytes.Equal(s.before, s.after) {
		if err := v.replaceAtomic(s.to, s.after, s.perm, nil); err != nil {
			return err
		}
		s.wrote = true
	}
	return nil
}

// mkdirsTracked creates the folders above rel and records the new ones, so
// a rollback can remove them.
func (v *Vault) mkdirsTracked(op, rel string, newDirs *[]string) error {
	var missing []string
	for d := path.Dir(rel); d != "." && d != ""; d = path.Dir(d) {
		if _, err := v.root.Lstat(d); errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, d)
		}
	}
	if err := v.mkdirs(op, rel, !strings.HasPrefix(rel, trashDir+"/")); err != nil {
		return err
	}
	*newDirs = append(*newDirs, missing...) // deepest first
	return nil
}

// rollback reverses the finished part of steps, newest first, then removes
// the folders the set created if they are empty again.
func (v *Vault) rollback(steps []*step, newDirs []string) error {
	var errs []error
	for i := len(steps) - 1; i >= 0; i-- {
		s := steps[i]
		switch s.c.Kind {
		case ChangeWrite:
			if s.wrote {
				errs = append(errs, v.replaceAtomic(s.from, s.before, s.perm, nil))
			}
		case ChangeCreate:
			if s.wrote {
				errs = append(errs, v.root.Remove(s.from))
			}
		case ChangeMove:
			if s.wrote {
				errs = append(errs, v.replaceAtomic(s.to, s.before, s.perm, nil))
			}
			if s.moved {
				errs = append(errs, v.moveNoReplace(s.to, s.from))
			}
		}
	}
	for _, d := range newDirs {
		_ = v.root.Remove(d) // only succeeds when empty
	}
	return errors.Join(errs...)
}

// CreateFolder creates the folder p and any missing parents. Each new
// folder name must work as a link target. An existing folder is left alone.
func (v *Vault) CreateFolder(ctx context.Context, p string, o WriteOptions) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rel, err := v.resolve("create_folder", p, accessRead)
	if err != nil {
		return nil, err
	}
	if rel == "" {
		return nil, &PathError{Op: "create_folder", Path: p, Reason: "the vault root already exists"}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	fi, err := v.lstatChain("create_folder", rel)
	switch {
	case err == nil && fi.IsDir():
		return &Result{Path: rel, DryRun: o.DryRun}, nil
	case err == nil:
		return nil, &PathError{Op: "create_folder", Path: rel, Reason: "a file has this path"}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	for seg := range strings.SplitSeq(rel, "/") {
		if reason := badName(seg); reason != "" {
			return nil, &PathError{Op: "create_folder", Path: rel, Reason: reason}
		}
	}
	res := &Result{Path: rel, Changed: true, DryRun: o.DryRun, Diff: "mkdir " + rel + "\n"}
	if o.DryRun {
		return res, nil
	}
	return res, v.mkdirs("create_folder", rel+"/x", true)
}

// AttachmentKinds are the kinds CreateAttachment writes.
var attachmentKinds = map[Kind]bool{KindImage: true, KindAudio: true, KindVideo: true, KindPDF: true}

// CreateAttachment writes a new image, audio, video or PDF file at p, the
// only way to write those kinds (V-ATT-01). If p is taken, " 1", " 2", …
// is added before the extension. The result's Path is the file written.
func (v *Vault) CreateAttachment(ctx context.Context, p string, data []byte, o WriteOptions) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rel, err := v.resolve("attach", p, accessRead)
	if err != nil {
		return nil, err
	}
	if !attachmentKinds[KindOf(rel)] {
		return nil, &PathError{Op: "attach", Path: rel, Reason: "attachments must be images, audio, video or PDF"}
	}
	if reason := badName(path.Base(rel)); reason != "" {
		return nil, &PathError{Op: "attach", Path: rel, Reason: reason}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	ext := path.Ext(rel)
	stem := strings.TrimSuffix(rel, ext)
	dst := rel
	for n := 1; ; n++ {
		_, err := v.lstatChain("attach", dst)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, err
		}
		if n > 10000 {
			return nil, fmt.Errorf("vault: attach: too many files named like %s", rel)
		}
		dst = fmt.Sprintf("%s %d%s", stem, n, ext)
	}
	res := &Result{Path: dst, ETag: ETag(data), Changed: true, DryRun: o.DryRun, Diff: fmt.Sprintf("create %s (%d bytes)\n", dst, len(data))}
	if o.DryRun {
		return res, nil
	}
	if err := v.mkdirs("attach", dst, true); err != nil {
		return nil, err
	}
	jdir, err := v.journalRecord(JournalEntry{Op: "create", Path: dst, ETagAfter: res.ETag}, nil)
	if err != nil {
		return nil, err
	}
	if err := v.createAtomic(dst, data, 0o644); err != nil {
		v.journalDrop(jdir)
		return nil, err
	}
	return res, nil
}
