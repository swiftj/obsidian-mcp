package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"path"
	"regexp"
	"strings"
	"time"
)

// TrashEntry is a file in the vault's .trash folder.
type TrashEntry struct {
	TrashPath    string // path inside .trash, as Restore takes it
	OriginalPath string // where Restore puts it back by default
	Size         int64
	ModTime      time.Time
}

// collisionSuffix matches the "~N" that Trash adds before the extension when
// .trash already holds a file with the same path.
var collisionSuffix = regexp.MustCompile(`~[0-9]+$`)

// originalPath recovers a file's path in the vault from its path in .trash.
func originalPath(trashPath string) string {
	dir, base := path.Split(trashPath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if s := collisionSuffix.ReplaceAllString(stem, ""); s != "" {
		stem = s
	}
	return dir + stem + ext
}

// Trash moves the file at p to .trash/<p> (V-TRASH-01). If .trash already
// holds that path, "~N" is added before the extension. etag must be the
// file's current etag.
func (v *Vault) Trash(ctx context.Context, p, etag string, o WriteOptions) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rel, err := v.resolve("trash", p, accessFile)
	if err != nil {
		return nil, err
	}
	if etag == "" {
		return nil, ErrETagRequired
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	cur, _, err := v.readRegular("trash", rel)
	if err != nil {
		return nil, err
	}
	if t := ETag(cur); t != etag {
		return nil, &ConflictError{Path: rel, Expected: etag, Current: t}
	}
	dst, err := v.freeTrashPath(rel)
	if err != nil {
		return nil, err
	}
	res := &Result{Path: dst, ETag: etag, Diff: fmt.Sprintf("rename %s → %s\n", rel, dst), DryRun: o.DryRun, Changed: true}
	if o.DryRun {
		return res, nil
	}
	if err := v.mkdirs("trash", dst, false); err != nil {
		return nil, err
	}
	jdir, err := v.journalRecord(JournalEntry{Op: "trash", Path: rel, NewPath: dst, ETagBefore: etag, ETagAfter: etag}, cur)
	if err != nil {
		return nil, err
	}
	if err := v.moveNoReplace(rel, dst); err != nil {
		v.journalDrop(jdir)
		return nil, err
	}
	return res, nil
}

// freeTrashPath returns .trash/<rel>, or the first free .trash/<stem>~N<ext>.
func (v *Vault) freeTrashPath(rel string) (string, error) {
	dst := trashDir + "/" + rel
	ext := path.Ext(dst)
	stem := strings.TrimSuffix(dst, ext)
	for n := 1; ; n++ {
		_, err := v.lstatChain("trash", dst)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return dst, nil
		case err != nil:
			return "", err
		case n > 10000:
			return "", fmt.Errorf("vault: trash: too many copies of %s", rel)
		}
		dst = fmt.Sprintf("%s~%d%s", stem, n, ext)
	}
}

// Restore moves a file from .trash back into the vault (V-TRASH-02).
// trashPath is a TrashEntry.TrashPath. to is the destination; "" means the
// entry's original path. It fails with *ConflictError if the destination is
// occupied.
func (v *Vault) Restore(ctx context.Context, trashPath, to string, o WriteOptions) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	trel, err := Clean(trashPath)
	if err != nil {
		err.(*PathError).Op = "restore"
		return nil, err
	}
	if to == "" {
		to = originalPath(trel)
	}
	dst, err := v.resolve("restore", to, accessFile)
	if err != nil {
		return nil, err
	}
	if reason := v.pol.hiddenReason(trel); reason != "" {
		return nil, &PathError{Op: "restore", Path: trashPath, Reason: reason}
	}
	src := trashDir + "/" + trel
	v.mu.Lock()
	defer v.mu.Unlock()
	data, _, err := v.readRegular("restore", src)
	if err != nil {
		if isNotFound(err) {
			return nil, &NotFoundError{Path: src}
		}
		return nil, err
	}
	if cur, _, err := v.readRegular("restore", dst); err == nil {
		return nil, &ConflictError{Path: dst, Current: ETag(cur)}
	} else if !isNotFound(err) {
		return nil, err
	}
	res := &Result{Path: dst, ETag: ETag(data), Diff: fmt.Sprintf("rename %s → %s\n", src, dst), DryRun: o.DryRun, Changed: true}
	if o.DryRun {
		return res, nil
	}
	if err := v.mkdirs("restore", dst, true); err != nil {
		return nil, err
	}
	jdir, err := v.journalRecord(JournalEntry{Op: "restore", Path: src, NewPath: dst, ETagBefore: res.ETag, ETagAfter: res.ETag}, nil)
	if err != nil {
		return nil, err
	}
	if err := v.moveNoReplace(src, dst); err != nil {
		v.journalDrop(jdir)
		if errors.Is(err, fs.ErrExist) {
			cur, _, _ := v.readRegular("restore", dst)
			return nil, &ConflictError{Path: dst, Current: ETag(cur)}
		}
		return nil, err
	}
	return res, nil
}

// ListTrash streams the files in .trash with their original paths
// (V-TRASH-03). Entries whose original path is hidden are skipped.
func (v *Vault) ListTrash(ctx context.Context) iter.Seq2[TrashEntry, error] {
	return func(yield func(TrashEntry, error) bool) {
		fi, err := v.lstatChain("list trash", trashDir)
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		if err == nil && !fi.IsDir() {
			err = &PathError{Op: "list trash", Path: trashDir, Reason: "not a folder"}
		}
		if err != nil {
			yield(TrashEntry{}, err)
			return
		}
		hide := func(p string) bool {
			return v.pol.hiddenReason(originalPath(strings.TrimPrefix(p, trashDir+"/"))) != ""
		}
		v.walk(ctx, trashDir, true, hide, func(e Entry, err error) bool {
			if err != nil {
				return yield(TrashEntry{}, err)
			}
			if e.Kind == KindFolder {
				return true
			}
			tp := strings.TrimPrefix(e.Path, trashDir+"/")
			return yield(TrashEntry{TrashPath: tp, OriginalPath: originalPath(tp), Size: e.Size, ModTime: e.ModTime}, nil)
		})
	}
}
