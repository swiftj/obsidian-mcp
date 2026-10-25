package vault

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Options configures a Vault. The zero value is valid.
type Options struct {
	// ConfigDir is the name of Obsidian's configuration folder at the vault
	// root. The default is ".obsidian". An override must start with ".".
	ConfigDir string
	// Deny holds glob patterns for paths that are invisible to every
	// operation. "**" matches any number of folders, and matching ignores
	// case. A pattern that matches a folder hides everything inside it.
	Deny []string
	// Journal keeps the previous bytes of every change for undo. Nil turns
	// journaling off.
	Journal *Journal
	// Logger receives debug events. Nil discards them.
	Logger *slog.Logger
}

// Vault is an open Obsidian vault. Its methods are safe for concurrent use;
// writes are serialised.
type Vault struct {
	name    string
	dir     string
	root    *os.Root
	pol     *policy
	journal *Journal
	log     *slog.Logger
	mu      sync.Mutex // serialises every write (V-WRITE-04)
}

// Open opens the vault folder dir.
func Open(ctx context.Context, dir string, opt Options) (*Vault, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	pol, err := newPolicy(opt.ConfigDir, opt.Deny)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("vault: open %s: %w", abs, err)
	}
	log := opt.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Vault{
		name:    filepath.Base(abs),
		dir:     abs,
		root:    root,
		pol:     pol,
		journal: opt.Journal,
		log:     log,
	}, nil
}

// Name returns the vault's name: its folder name.
func (v *Vault) Name() string { return v.name }

// Dir returns the vault's absolute folder path.
func (v *Vault) Dir() string { return v.dir }

// Close releases the vault's root handle.
func (v *Vault) Close() error { return v.root.Close() }
