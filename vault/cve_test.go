package vault

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCVERegressions has one case per path-sandbox advisory pattern listed in
// CLAUDE.md §1 (V-PATH-11). Every case must be refused and must leave the
// file outside the vault untouched.
func TestCVERegressions(t *testing.T) {
	ctx := context.Background()
	v, dir := openBasic(t, Options{})
	parent := filepath.Dir(dir)
	outside := filepath.Join(parent, "outside.md")
	if err := os.WriteFile(outside, []byte("secret outside the vault\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sibling folder whose name shares the vault's prefix (prefix-check bypass).
	evil := dir + "-evil"
	if err := os.MkdirAll(evil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evil, "x.md"), []byte("evil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Symlinks inside the vault pointing out, at file and folder level.
	if err := os.Symlink(outside, filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(parent, filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "Folder"), filepath.Join(dir, "inner")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string // advisory pattern
		path string
	}{
		{"traversal (mcpvault CVE-2026-57441)", "../outside.md"},
		{"nested traversal", "Folder/../../outside.md"},
		{"encoded traversal (Local REST API GHSA-62gx)", "%2e%2e/outside.md"},
		{"encoded slash", "..%2foutside.md"},
		{"absolute path (CVE-2026-19331)", outside},
		{"sibling prefix (mcpvault CVE-2026-57442)", "../" + filepath.Base(evil) + "/x.md"},
		{"symlinked file escape (mcpvault GHSA-97wj)", "link.md"},
		{"symlinked folder escape", "linkdir/outside.md"},
		{"symlink inside the vault", "inner/Sub/Deep.md"},
		{"backslash traversal", `..\outside.md`},
		{"NUL truncation", "Other.md\x00.png"},
		{"config read, case variant (Local REST API GHSA-j9qv)", ".OBSIDIAN/app.json"},
		{"config write, plugin code (Local REST API GHSA-66m9, RCE)", ".obsidian/plugins/evil/main.md"},
		{"git folder", ".git/config.md"},
		{"trash bypass", ".trash/Old.md"},
		{"trailing dot alias", "Other.md."},
		{"trailing space alias", "Other.md "},
	}
	before := readOutside(t, parent, dir)
	for _, c := range cases {
		if _, err := v.Read(ctx, c.path); err == nil {
			t.Errorf("V-PATH-11 %s: read %q allowed", c.name, c.path)
		}
		if _, err := v.Create(ctx, c.path, []byte("pwned\n"), WriteOptions{}); err == nil {
			t.Errorf("V-PATH-11 %s: create %q allowed", c.name, c.path)
		}
		if _, err := v.Replace(ctx, c.path, []byte("pwned\n"), ETag([]byte("secret outside the vault\n")), WriteOptions{}); err == nil {
			t.Errorf("V-PATH-11 %s: replace %q allowed", c.name, c.path)
		}
		if _, err := v.Trash(ctx, c.path, "x", WriteOptions{}); err == nil {
			t.Errorf("V-PATH-11 %s: trash %q allowed", c.name, c.path)
		}
	}
	if after := readOutside(t, parent, dir); after != before {
		t.Errorf("V-PATH-11 files outside the vault changed:\n%s\n---\n%s", before, after)
	}
	// V-PATH-05: symlinks are never listed or followed.
	for _, p := range listPaths(t, v, "", true) {
		if p == "link.md" || strings.HasPrefix(p, "linkdir") || strings.HasPrefix(p, "inner") {
			t.Errorf("V-PATH-05 symlink listed: %s", p)
		}
	}
	var pe *PathError
	if _, err := v.Read(ctx, "inner/Sub/Deep.md"); !errors.As(err, &pe) || !strings.Contains(pe.Reason, "symlink") {
		t.Errorf("V-PATH-05 symlinked folder: %v", err)
	}
}

// readOutside snapshots every file under parent except the vault itself.
func readOutside(t *testing.T, parent, vaultDir string) string {
	t.Helper()
	var sb strings.Builder
	err := filepath.WalkDir(parent, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == vaultDir {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sb.WriteString(p + "\n" + string(b))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

// TestOnlyRootIO checks V-PATH-06: library code reaches the vault only
// through os.Root. The only os file calls allowed are opening roots and the
// journal folder, which lives outside the vault.
func TestOnlyRootIO(t *testing.T) {
	banned := map[string]bool{
		"Open": true, "OpenFile": true, "Create": true, "ReadFile": true, "WriteFile": true,
		"ReadDir": true, "Stat": true, "Lstat": true, "Remove": true, "RemoveAll": true,
		"Rename": true, "Link": true, "Symlink": true, "Readlink": true, "Mkdir": true,
		"MkdirAll": true, "Chmod": true, "Chtimes": true, "Truncate": true, "CopyFS": true, "DirFS": true,
	}
	allowed := map[string]bool{"journal.go:MkdirAll": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "os" && banned[sel.Sel.Name] && !allowed[f+":"+sel.Sel.Name] {
				t.Errorf("V-PATH-06 %s: os.%s bypasses os.Root", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}
