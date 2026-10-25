package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// File is the content of a file read from the vault.
type File struct {
	Path    string
	Data    []byte
	ETag    string
	ModTime time.Time
	Kind    Kind
}

// ETag returns the etag of data: its lowercase hex sha256 (V-READ-01).
func ETag(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Read reads the file at p.
func (v *Vault) Read(ctx context.Context, p string) (*File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rel, err := v.resolve("read", p, accessRead)
	if err != nil {
		return nil, err
	}
	data, fi, err := v.readRegular("read", rel)
	if err != nil {
		return nil, err
	}
	return &File{Path: rel, Data: data, ETag: ETag(data), ModTime: fi.ModTime(), Kind: KindOf(rel)}, nil
}
