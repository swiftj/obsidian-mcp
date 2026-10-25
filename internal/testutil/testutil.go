// Package testutil holds helpers shared by tests.
package testutil

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RepoRoot returns the repository's root folder.
func RepoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// CopyVault copies testdata/vaults/<name> into a fresh temporary folder and
// returns that folder's path.
func CopyVault(t testing.TB, name string) string {
	t.Helper()
	src := filepath.Join(RepoRoot(), "testdata", "vaults", name)
	dst := filepath.Join(t.TempDir(), name)
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy vault %s: %v", name, err)
	}
	return dst
}

// TreeSums returns the sha256 of every file under dir, keyed by its
// slash-separated relative path, plus an entry for every folder and symlink.
func TreeSums(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			out[rel] = "symlink:" + target
		case d.IsDir():
			out[rel] = "dir"
		default:
			b, err := os.ReadFile(p) //nolint:gosec // reads files under a test folder it is walking
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			out[rel] = hex.EncodeToString(sum[:])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("tree sums: %v", err)
	}
	return out
}
