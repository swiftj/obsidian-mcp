// Package scan keeps a parsed copy of a vault's notes and canvases, and
// refreshes it incrementally by modification time and size. The link and
// search indexes build on it.
package scan

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/swiftj/obsidian-mcp/vault"
	"github.com/swiftj/obsidian-mcp/vault/canvas"
	"github.com/swiftj/obsidian-mcp/vault/ofm"
	"github.com/swiftj/obsidian-mcp/vault/props"
)

// File is one visible vault file. Doc and Props are set for readable notes,
// Canvas for readable canvases.
type File struct {
	Path    string
	Kind    vault.Kind
	ModTime time.Time
	Size    int64
	Doc     *ofm.Doc
	Props   *props.Frontmatter
	Canvas  []CanvasFile
}

// CanvasFile is a canvas "file" node.
type CanvasFile struct {
	ID      string
	File    string
	Subpath string
}

// Changes says what a Refresh found.
type Changes struct {
	Changed []string // added or modified, sorted
	Removed []string // sorted
}

// Empty reports whether nothing changed.
func (c Changes) Empty() bool { return len(c.Changed) == 0 && len(c.Removed) == 0 }

// SetChanged reports whether files were added or removed, which can change
// how links resolve.
func (c Changes) SetChanged(before map[string]bool) bool {
	if len(c.Removed) > 0 {
		return true
	}
	for _, p := range c.Changed {
		if !before[p] {
			return true
		}
	}
	return false
}

// Scanner is not safe for concurrent use; its owner serialises calls.
type Scanner struct {
	v     *vault.Vault
	log   *slog.Logger
	files map[string]*File
	// Reads counts the files read so far, for tests.
	Reads int
}

// New returns a scanner over v that has read nothing yet. log may be nil.
func New(v *vault.Vault, log *slog.Logger) *Scanner {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Scanner{v: v, log: log, files: map[string]*File{}}
}

// Files returns the current files by path. The map must not be changed.
func (s *Scanner) Files() map[string]*File { return s.files }

// Paths returns the current file paths, sorted.
func (s *Scanner) Paths() []string { return slices.Sorted(maps.Keys(s.files)) }

// Refresh lists the vault and re-reads every file that is new or whose
// modification time or size changed. An unreadable file is kept without
// content and retried on the next refresh.
func (s *Scanner) Refresh(ctx context.Context) (Changes, error) {
	var ch Changes
	seen := make(map[string]bool, len(s.files))
	for e, err := range s.v.List(ctx, "", true) {
		if err != nil {
			return Changes{}, err
		}
		if e.Kind == vault.KindFolder {
			continue
		}
		seen[e.Path] = true
		if old := s.files[e.Path]; old != nil && old.ModTime.Equal(e.ModTime) && old.Size == e.Size {
			continue
		}
		f := &File{Path: e.Path, Kind: e.Kind, ModTime: e.ModTime, Size: e.Size}
		if e.Kind == vault.KindMarkdown || e.Kind == vault.KindCanvas {
			if !s.load(ctx, f) {
				f.ModTime = time.Time{} // retry next time
			}
		}
		s.files[e.Path] = f
		ch.Changed = append(ch.Changed, e.Path)
	}
	for p := range s.files {
		if !seen[p] {
			delete(s.files, p)
			ch.Removed = append(ch.Removed, p)
		}
	}
	slices.Sort(ch.Changed)
	slices.Sort(ch.Removed)
	return ch, nil
}

func (s *Scanner) load(ctx context.Context, f *File) bool {
	s.Reads++
	r, err := s.v.Read(ctx, f.Path)
	if err != nil {
		s.log.Debug("scan: skipping unreadable file", "path", f.Path, "err", err)
		return false
	}
	parse(f, r.Data)
	return true
}

// Parse returns the parsed form of a note or canvas's bytes, the same way a
// refresh parses it. Other kinds get no content.
func Parse(p string, data []byte) *File {
	f := &File{Path: p, Kind: vault.KindOf(p)}
	parse(f, data)
	return f
}

func parse(f *File, data []byte) {
	switch f.Kind {
	case vault.KindMarkdown:
		f.Doc = ofm.Parse(data)
		f.Props = props.Read(f.Doc)
	case vault.KindCanvas:
		c, err := canvas.Parse(data)
		if err != nil {
			return // an invalid canvas has no links
		}
		for _, n := range c.Nodes() {
			if n.Type == "file" && n.File != "" {
				f.Canvas = append(f.Canvas, CanvasFile{ID: n.ID, File: n.File, Subpath: n.Subpath})
			}
		}
	}
}
