package vault

import (
	"context"
	"io/fs"
	"iter"
	"path"
	"slices"
	"strings"
	"time"
)

// Kind classifies a vault entry by extension (file-formats.md).
type Kind string

const (
	KindFolder   Kind = "folder"
	KindMarkdown Kind = "md"
	KindCanvas   Kind = "canvas"
	KindBase     Kind = "base"
	KindImage    Kind = "image"
	KindAudio    Kind = "audio"
	KindVideo    Kind = "video"
	KindPDF      Kind = "pdf"
	KindOther    Kind = "other"
)

// KindOf classifies a file name by its extension, ignoring case. "webm" is
// listed as both audio and video by Obsidian; it is classified as video.
func KindOf(name string) Kind {
	switch strings.ToLower(strings.TrimPrefix(path.Ext(name), ".")) {
	case "md":
		return KindMarkdown
	case "canvas":
		return KindCanvas
	case "base":
		return KindBase
	case "avif", "bmp", "gif", "jpeg", "jpg", "png", "svg", "webp":
		return KindImage
	case "flac", "m4a", "mp3", "ogg", "wav", "3gp":
		return KindAudio
	case "mkv", "mov", "mp4", "ogv", "webm":
		return KindVideo
	case "pdf":
		return KindPDF
	}
	return KindOther
}

// Entry is one file or folder in a listing.
type Entry struct {
	Path    string
	Kind    Kind
	Size    int64
	ModTime time.Time
}

// List streams the entries of folder dir ("" for the vault root), sorted by
// name within each folder, descending into subfolders when recursive is set.
// Hidden and denied paths, names starting with ".", and symlinks are skipped
// without being descended into (V-LIST-01). An error ends the stream.
func (v *Vault) List(ctx context.Context, dir string, recursive bool) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) {
		rel := dir
		if dir != "" {
			var err error
			if rel, err = v.resolve("list", dir, accessRead); err != nil {
				yield(Entry{}, err)
				return
			}
		}
		fi, err := v.lstatChain("list", rel)
		if err != nil {
			yield(Entry{}, v.notFound(rel, err))
			return
		}
		if !fi.IsDir() {
			yield(Entry{}, &PathError{Op: "list", Path: rel, Reason: "not a folder"})
			return
		}
		v.walk(ctx, rel, recursive, v.hidden, yield)
	}
}

func (v *Vault) hidden(p string) bool { return v.pol.hiddenReason(p) != "" }

// walk yields the entries of folder rel, skipping names starting with ".",
// symlinks and paths for which hide is true. It returns false once yield has
// asked to stop or an error has been yielded.
func (v *Vault) walk(ctx context.Context, rel string, recursive bool, hide func(string) bool, yield func(Entry, error) bool) bool {
	if err := ctx.Err(); err != nil {
		yield(Entry{}, err)
		return false
	}
	name := rel
	if name == "" {
		name = "."
	}
	d, err := v.root.Open(name)
	if err != nil {
		yield(Entry{}, err)
		return false
	}
	ents, err := d.ReadDir(-1)
	_ = d.Close()
	if err != nil {
		yield(Entry{}, err)
		return false
	}
	slices.SortFunc(ents, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, de := range ents {
		if strings.HasPrefix(de.Name(), ".") || de.Type()&fs.ModeSymlink != 0 {
			continue
		}
		p := path.Join(rel, de.Name())
		if hide(p) {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue // removed while listing
		}
		e := Entry{Path: p, ModTime: info.ModTime()}
		switch {
		case info.IsDir():
			e.Kind = KindFolder
		case info.Mode().IsRegular():
			e.Kind, e.Size = KindOf(p), info.Size()
		default:
			continue
		}
		if !yield(e, nil) {
			return false
		}
		if recursive && e.Kind == KindFolder && !v.walk(ctx, p, recursive, hide, yield) {
			return false
		}
	}
	return true
}
