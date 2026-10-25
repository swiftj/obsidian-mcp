package vault

import (
	"fmt"
	"strconv"
)

// PathError reports a path refused by the sandbox or the vault's policy.
type PathError struct {
	Op     string // the operation, such as "read" or "write"
	Path   string // the path as given by the caller
	Reason string
}

func (e *PathError) Error() string {
	return fmt.Sprintf("vault: %s %s: %s", e.Op, strconv.Quote(e.Path), e.Reason)
}

// ConflictError reports that a file is not in the state the caller expected:
// its etag changed since it was read, or it exists when it was to be created.
type ConflictError struct {
	Path     string
	Expected string // the etag the caller passed; "" for a create
	Current  string // the file's etag now
}

func (e *ConflictError) Error() string {
	if e.Expected == "" {
		return fmt.Sprintf("vault: %s already exists (etag %s)", strconv.Quote(e.Path), e.Current)
	}
	return fmt.Sprintf("vault: %s changed since it was read (etag %s, now %s); re-read it and retry",
		strconv.Quote(e.Path), e.Expected, e.Current)
}

// NotFoundError reports a path that does not exist.
type NotFoundError struct {
	Path string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("vault: %s not found", strconv.Quote(e.Path))
}
