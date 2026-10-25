// Package vault reads and safely writes an Obsidian vault directly on disk.
//
// Every path is vault-relative, uses "/" and is checked by Clean and the
// vault's policy before any file is touched. All file access goes through a
// single [os.Root], and symlinks are refused. Changes to existing files need
// the etag returned by a read, writes are atomic, and deletes go to the
// vault's .trash folder.
package vault
