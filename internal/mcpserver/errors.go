package mcpserver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/swiftj/obsidian-mcp/vault"
	"github.com/swiftj/obsidian-mcp/vault/links"
	"github.com/swiftj/obsidian-mcp/vault/props"
)

// toolError turns a library error into a tool error with an actionable hint
// (X-SRV-10). The SDK returns it as a result with isError set.
func toolError(err error) error {
	var (
		conflict *vault.ConflictError
		notFound *vault.NotFoundError
		pathErr  *vault.PathError
		ambig    *links.AmbiguousError
		editErr  *vault.EditError
		propErr  *props.Error
	)
	hint := ""
	switch {
	case errors.As(err, &conflict):
		if conflict.Expected == "" {
			hint = "the file already exists; read it, then edit or replace it with its etag, or pick another path"
		} else {
			hint = "the file changed since you read it; call read_note again and retry with the new etag"
		}
	case errors.Is(err, vault.ErrETagRequired):
		hint = "read the note first and pass its etag"
	case errors.As(err, &notFound):
		hint = "check the path with list_notes or search; paths are relative to the vault root and include the extension"
	case errors.As(err, &pathErr):
		hint = "paths are relative to the vault root, use / between folders, and can't reach hidden folders"
	case errors.As(err, &ambig):
		hint = "several files match; use one of the full paths: " + strings.Join(ambig.Candidates, ", ")
	case errors.As(err, &editErr):
		if len(editErr.Candidates) > 0 {
			hint = "use a longer heading path (Parent#Heading) or one of: " + strings.Join(editErr.Candidates, "; ")
		} else {
			hint = "call get_note_info for the note's headings and block IDs, or read_note for its text"
		}
	case errors.As(err, &propErr):
		hint = propErr.Hint
	}
	if hint == "" {
		return err
	}
	return fmt.Errorf("%w. Hint: %s", err, hint)
}
