package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// DefaultJournalCap is the journal's default size cap: 500 MB (D-001).
const DefaultJournalCap int64 = 500 << 20

// DefaultJournalDir returns ~/.local/state/obs-mcp/journal.
func DefaultJournalDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("vault: journal: %w", err)
	}
	return filepath.Join(home, ".local", "state", "obs-mcp", "journal"), nil
}

// Journal keeps the previous bytes of every change, outside the vault, so a
// change can be undone (V-JRN-01). Entries are pruned oldest first once the
// journal grows past its cap (V-JRN-02). One Journal can serve several vaults.
type Journal struct {
	root *os.Root
	cap  int64
	mu   sync.Mutex
	seq  uint64
}

// JournalEntry describes one recorded change.
type JournalEntry struct {
	ID         string    `json:"id"`
	Vault      string    `json:"vault"`
	Op         string    `json:"op"` // create, replace, trash or restore
	Path       string    `json:"path"`
	NewPath    string    `json:"new_path,omitempty"` // where trash and restore moved the file
	Time       time.Time `json:"time"`
	ETagBefore string    `json:"etag_before,omitempty"` // "" when the file did not exist
	HasData    bool      `json:"has_data"`              // whether the previous bytes are kept
}

// OpenJournal opens or creates the journal folder dir. capBytes <= 0 means
// DefaultJournalCap.
func OpenJournal(dir string, capBytes int64) (*Journal, error) {
	if capBytes <= 0 {
		capBytes = DefaultJournalCap
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("vault: journal: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("vault: journal: %w", err)
	}
	return &Journal{root: root, cap: capBytes}, nil
}

// Close releases the journal's folder handle.
func (j *Journal) Close() error { return j.root.Close() }

// vaultKey names a vault's journal folder: its name plus a hash of its
// absolute path, so two vaults with the same name don't mix.
func vaultKey(v *Vault) string {
	sum := sha256.Sum256([]byte(v.dir))
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 0x20 {
			return '_'
		}
		return r
	}, v.name)
	return strings.TrimLeft(name, ".") + "-" + hex.EncodeToString(sum[:4])
}

// record stores e and the previous bytes, then prunes. It returns the
// entry's folder so a failed change can drop it again.
func (j *Journal) record(v *Vault, e JournalEntry, before []byte) (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	e.Vault = v.name
	e.Time = time.Now().UTC()
	e.ID = fmt.Sprintf("%020d-%06d", e.Time.UnixNano(), j.seq%1_000_000)
	e.HasData = before != nil
	dir := path.Join(vaultKey(v), e.ID)
	if err := j.root.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("vault: journal: %w", err)
	}
	meta, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	if before != nil {
		if err := j.root.WriteFile(path.Join(dir, "data"), before, 0o600); err != nil {
			_ = j.root.RemoveAll(dir)
			return "", fmt.Errorf("vault: journal: %w", err)
		}
	}
	if err := j.root.WriteFile(path.Join(dir, "meta.json"), meta, 0o600); err != nil {
		_ = j.root.RemoveAll(dir)
		return "", fmt.Errorf("vault: journal: %w", err)
	}
	if err := j.prune(); err != nil {
		return "", err
	}
	return dir, nil
}

// drop removes an entry recorded for a change that then failed.
func (j *Journal) drop(dir string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	_ = j.root.RemoveAll(dir)
}

type entryDir struct {
	dir  string // <vault key>/<id>
	id   string
	size int64
}

func (j *Journal) entryDirs(vaultKey string) ([]entryDir, error) {
	var out []entryDir
	keys := []string{vaultKey}
	if vaultKey == "" {
		ents, err := fs.ReadDir(j.root.FS(), ".")
		if err != nil {
			return nil, err
		}
		keys = keys[:0]
		for _, e := range ents {
			if e.IsDir() {
				keys = append(keys, e.Name())
			}
		}
	}
	for _, k := range keys {
		ents, err := fs.ReadDir(j.root.FS(), k)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			d := path.Join(k, e.Name())
			var size int64
			for _, f := range []string{"meta.json", "data"} {
				if fi, err := j.root.Lstat(path.Join(d, f)); err == nil {
					size += fi.Size()
				}
			}
			out = append(out, entryDir{dir: d, id: e.Name(), size: size})
		}
	}
	slices.SortFunc(out, func(a, b entryDir) int { return strings.Compare(a.id, b.id) })
	return out, nil
}

// prune removes the oldest entries, across all vaults, until the journal is
// within its cap. The caller holds j.mu.
func (j *Journal) prune() error {
	ents, err := j.entryDirs("")
	if err != nil {
		return fmt.Errorf("vault: journal: %w", err)
	}
	var total int64
	for _, e := range ents {
		total += e.size
	}
	for i := 0; total > j.cap && i < len(ents)-1; i++ { // always keep the newest
		if err := j.root.RemoveAll(ents[i].dir); err != nil {
			return fmt.Errorf("vault: journal: %w", err)
		}
		total -= ents[i].size
	}
	return nil
}

// Entries returns v's journal entries, newest first.
func (j *Journal) Entries(v *Vault) ([]JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	dirs, err := j.entryDirs(vaultKey(v))
	if err != nil {
		return nil, fmt.Errorf("vault: journal: %w", err)
	}
	out := make([]JournalEntry, 0, len(dirs))
	for i := len(dirs) - 1; i >= 0; i-- {
		b, err := j.root.ReadFile(path.Join(dirs[i].dir, "meta.json"))
		if err != nil {
			continue // pruned or half-written
		}
		var e JournalEntry
		if json.Unmarshal(b, &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// Data returns the previous bytes kept for entry id of v.
func (j *Journal) Data(v *Vault, id string) ([]byte, error) {
	if strings.ContainsAny(id, "/\\") || id == "" || id[0] == '.' {
		return nil, fmt.Errorf("vault: journal: bad entry id %q", id)
	}
	b, err := j.root.ReadFile(path.Join(vaultKey(v), id, "data"))
	if err != nil {
		return nil, fmt.Errorf("vault: journal: %w", err)
	}
	return b, nil
}
