package vault

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path"
	"unicode/utf8"

	"github.com/swiftj/obsidian-mcp/internal/udiff"
)

// ErrETagRequired is returned when a change to an existing file has no etag.
var ErrETagRequired = errors.New("vault: an etag from a previous read is required to change an existing file")

// WriteOptions modifies a write.
type WriteOptions struct {
	// DryRun computes the change and its diff but writes nothing.
	DryRun bool
}

// Result describes a write, or the write a dry run would have done.
type Result struct {
	Path    string
	ETag    string // the file's etag after the write
	Diff    string // unified diff of the change; "" when nothing changed
	DryRun  bool
	Changed bool
}

var bom = []byte{0xEF, 0xBB, 0xBF}

// MatchStyle returns data with old's line endings and byte-order mark
// (V-WRITE-05): if old uses CRLF, every line ending in data becomes CRLF, and
// otherwise LF; a UTF-8 BOM is kept if old has one and dropped if not.
func MatchStyle(old, data []byte) []byte {
	hadBOM := bytes.HasPrefix(old, bom)
	data = bytes.TrimPrefix(data, bom)
	out := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if usesCRLF(old) {
		out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))
	}
	if hadBOM {
		out = append(append([]byte{}, bom...), out...)
	}
	return out
}

// usesCRLF reports whether data's first line ending is CRLF.
func usesCRLF(data []byte) bool {
	i := bytes.IndexByte(data, '\n')
	return i > 0 && data[i-1] == '\r'
}

func checkText(op, rel string, data []byte) error {
	if path.Ext(rel) == ".md" && !utf8.Valid(data) {
		return &PathError{Op: op, Path: rel, Reason: "content is not valid UTF-8"}
	}
	return nil
}

// Create writes a new file at p. It fails with *ConflictError if p exists
// (V-WRITE-02). Missing folders are created; their names, and the file's,
// must work as link targets (V-WRITE-08).
func (v *Vault) Create(ctx context.Context, p string, data []byte, o WriteOptions) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rel, err := v.resolve("create", p, accessWrite)
	if err != nil {
		return nil, err
	}
	if reason := badName(path.Base(rel)); reason != "" {
		return nil, &PathError{Op: "create", Path: rel, Reason: reason}
	}
	if err := checkText("create", rel, data); err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if cur, _, err := v.readRegular("create", rel); err == nil {
		return nil, &ConflictError{Path: rel, Current: ETag(cur)}
	} else if !isNotFound(err) {
		return nil, err
	}
	res := &Result{Path: rel, ETag: ETag(data), Diff: udiff.Unified(rel, nil, data), DryRun: o.DryRun, Changed: true}
	if o.DryRun {
		return res, nil
	}
	if err := v.mkdirs("create", rel, true); err != nil {
		return nil, err
	}
	jdir, err := v.journalRecord(JournalEntry{Op: "create", Path: rel, ETagAfter: res.ETag}, nil)
	if err != nil {
		return nil, err
	}
	if err := v.createAtomic(rel, data, 0o644); err != nil {
		v.journalDrop(jdir)
		if errors.Is(err, fs.ErrExist) {
			cur, _, _ := v.readRegular("create", rel)
			return nil, &ConflictError{Path: rel, Current: ETag(cur)}
		}
		return nil, err
	}
	return res, nil
}

// Replace replaces the whole content of the existing file at p. etag must be
// the file's current etag (V-WRITE-01). data is converted to the file's line
// endings and BOM (V-WRITE-05).
func (v *Vault) Replace(ctx context.Context, p string, data []byte, etag string, o WriteOptions) (*Result, error) {
	return v.replace(ctx, p, etag, o, func(cur []byte) ([]byte, error) { return MatchStyle(cur, data), nil })
}

// Update changes the existing file at p to change(current bytes), checked
// against etag and written like Replace. change runs under the write lock
// and its result is written as is; canvas and base edits use it.
func (v *Vault) Update(ctx context.Context, p, etag string, change func(cur []byte) ([]byte, error), o WriteOptions) (*Result, error) {
	return v.replace(ctx, p, etag, o, change)
}

// replace changes the existing file at p to change(current bytes), with
// the etag check, journal, atomic write and dry run of Replace. change runs
// under the write lock, on the bytes the etag was checked against.
func (v *Vault) replace(ctx context.Context, p, etag string, o WriteOptions, change func(cur []byte) ([]byte, error)) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rel, err := v.resolve("replace", p, accessWrite)
	if err != nil {
		return nil, err
	}
	if etag == "" {
		return nil, ErrETagRequired
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	cur, fi, err := v.readRegular("replace", rel)
	if err != nil {
		return nil, err
	}
	curTag := ETag(cur)
	if curTag != etag {
		return nil, &ConflictError{Path: rel, Expected: etag, Current: curTag}
	}
	data, err := change(cur)
	if err != nil {
		return nil, err
	}
	if err := checkText("replace", rel, data); err != nil {
		return nil, err
	}
	res := &Result{Path: rel, ETag: ETag(data), DryRun: o.DryRun, Changed: !bytes.Equal(cur, data)}
	if !res.Changed {
		return res, nil
	}
	res.Diff = udiff.Unified(rel, cur, data)
	if o.DryRun {
		return res, nil
	}
	jdir, err := v.journalRecord(JournalEntry{Op: "replace", Path: rel, ETagBefore: curTag, ETagAfter: res.ETag}, cur)
	if err != nil {
		return nil, err
	}
	err = v.replaceAtomic(rel, data, fi.Mode().Perm(), func() error {
		now, _, err := v.readRegular("replace", rel)
		if err != nil {
			return err
		}
		if t := ETag(now); t != curTag {
			return &ConflictError{Path: rel, Expected: etag, Current: t}
		}
		return nil
	})
	if err != nil {
		v.journalDrop(jdir)
		return nil, err
	}
	return res, nil
}

func isNotFound(err error) bool {
	var nf *NotFoundError
	return errors.As(err, &nf)
}

func (v *Vault) journalRecord(e JournalEntry, before []byte) (string, error) {
	if v.journal == nil {
		return "", nil
	}
	return v.journal.record(v, e, before)
}

func (v *Vault) journalDrop(dir string) {
	if v.journal != nil && dir != "" {
		v.journal.drop(dir)
	}
}
