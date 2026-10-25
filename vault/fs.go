package vault

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"syscall"
)

// resolve checks p with Clean and the policy, and returns the clean path.
func (v *Vault) resolve(op, p string, a access) (string, error) {
	rel, err := Clean(p)
	if err != nil {
		err.(*PathError).Op = op
		return "", err
	}
	if err := v.pol.check(op, rel, a); err != nil {
		return "", err
	}
	return rel, nil
}

// lstatChain checks every component of rel with Lstat and refuses symlinks
// (V-PATH-05). It returns the info for rel, or an error wrapping
// fs.ErrNotExist naming the first missing component.
func (v *Vault) lstatChain(op, rel string) (fs.FileInfo, error) {
	if rel == "" || rel == "." {
		return v.root.Lstat(".")
	}
	var info fs.FileInfo
	segs := strings.Split(rel, "/")
	for i := range segs {
		prefix := strings.Join(segs[:i+1], "/")
		fi, err := v.root.Lstat(prefix)
		if err != nil {
			return nil, err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return nil, &PathError{Op: op, Path: rel, Reason: "symlink at " + prefix}
		}
		if i < len(segs)-1 && !fi.IsDir() {
			return nil, &PathError{Op: op, Path: rel, Reason: prefix + " is not a folder"}
		}
		info = fi
	}
	return info, nil
}

// readRegular reads a regular file after checking its path for symlinks, and
// opens it with O_NOFOLLOW so a swapped-in symlink is not followed.
func (v *Vault) readRegular(op, rel string) ([]byte, fs.FileInfo, error) {
	if _, err := v.lstatChain(op, rel); err != nil {
		return nil, nil, v.notFound(rel, err)
	}
	f, err := v.root.OpenFile(rel, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, v.notFound(rel, err)
	}
	defer f.Close() //nolint:errcheck // read-only file
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, nil, &PathError{Op: op, Path: rel, Reason: "not a regular file"}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}
	return data, fi, nil
}

// notFound turns fs.ErrNotExist into *NotFoundError and passes other errors on.
func (v *Vault) notFound(rel string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return &NotFoundError{Path: rel}
	}
	return err
}

// tempName returns a fresh hidden temp-file path next to rel.
func tempName(rel string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return path.Join(path.Dir(rel), "."+path.Base(rel)+".obs-mcp-"+hex.EncodeToString(b[:])+".tmp"), nil
}

// writeTemp writes data to a new temp file next to rel, with mode perm, and
// fsyncs it.
func (v *Vault) writeTemp(rel string, data []byte, perm fs.FileMode) (string, error) {
	tmp, err := tempName(rel)
	if err != nil {
		return "", err
	}
	f, err := v.root.OpenFile(tmp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW, perm)
	if err != nil {
		return "", err
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Chmod(perm) // the umask may have narrowed it
	}
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = v.root.Remove(tmp)
		return "", werr
	}
	return tmp, nil
}

// replaceAtomic writes data over rel: temp file, fsync, rename, fsync folder
// (V-WRITE-03). check runs just before the rename; an error from it aborts
// the write.
func (v *Vault) replaceAtomic(rel string, data []byte, perm fs.FileMode, check func() error) error {
	tmp, err := v.writeTemp(rel, data, perm)
	if err != nil {
		return err
	}
	if check != nil {
		if err := check(); err != nil {
			_ = v.root.Remove(tmp)
			return err
		}
	}
	if err := v.root.Rename(tmp, rel); err != nil {
		_ = v.root.Remove(tmp)
		return err
	}
	return v.syncDir(path.Dir(rel))
}

// createAtomic writes a new file at rel and fails if rel exists
// (V-WRITE-02). The temp file is hard-linked into place, so the target never
// appears half-written and is never overwritten.
func (v *Vault) createAtomic(rel string, data []byte, perm fs.FileMode) error {
	tmp, err := v.writeTemp(rel, data, perm)
	if err != nil {
		return err
	}
	defer v.root.Remove(tmp) //nolint:errcheck // best effort; the link holds the data
	if err := v.root.Link(tmp, rel); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return err
		}
		// Some filesystems have no hard links: fall back to an exclusive create.
		f, ferr := v.root.OpenFile(rel, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW, perm)
		if ferr != nil {
			return ferr
		}
		_, werr := f.Write(data)
		if werr == nil {
			werr = f.Sync()
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = v.root.Remove(rel)
			return fmt.Errorf("vault: create %s: %w", rel, werr)
		}
	}
	return v.syncDir(path.Dir(rel))
}

// moveNoReplace moves from to to, failing with an error wrapping
// fs.ErrExist if to exists. It links then unlinks, so an existing file is
// never overwritten; without hard links it checks and renames.
func (v *Vault) moveNoReplace(from, to string) error {
	if err := v.root.Link(from, to); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return err
		}
		if _, serr := v.root.Lstat(to); serr == nil {
			return fs.ErrExist
		}
		if err := v.root.Rename(from, to); err != nil {
			return err
		}
	} else if err := v.root.Remove(from); err != nil {
		_ = v.root.Remove(to)
		return err
	}
	if err := v.syncDir(path.Dir(to)); err != nil {
		return err
	}
	return v.syncDir(path.Dir(from))
}

// syncDir fsyncs a folder so a rename in it is durable. Filesystems that
// can't sync folders are tolerated.
func (v *Vault) syncDir(dir string) error {
	d, err := v.root.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EBADF) {
		return nil
	}
	return err
}

// mkdirs creates the missing folders above rel. Existing ones must not be
// symlinks. With checkNames, each new folder name must also be a valid link
// target (V-WRITE-08).
func (v *Vault) mkdirs(op, rel string, checkNames bool) error {
	dir := path.Dir(rel)
	if dir == "." {
		return nil
	}
	segs := strings.Split(dir, "/")
	for i := range segs {
		prefix := strings.Join(segs[:i+1], "/")
		fi, err := v.root.Lstat(prefix)
		switch {
		case err == nil && fi.Mode()&fs.ModeSymlink != 0:
			return &PathError{Op: op, Path: rel, Reason: "symlink at " + prefix}
		case err == nil && !fi.IsDir():
			return &PathError{Op: op, Path: rel, Reason: prefix + " is not a folder"}
		case err == nil:
			continue
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
		if reason := badName(segs[i]); checkNames && reason != "" {
			return &PathError{Op: op, Path: rel, Reason: reason}
		}
		if err := v.root.Mkdir(prefix, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
	}
	return nil
}
