package vault

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestErrorTypes covers V-ERR-01 for the vault errors. *links.AmbiguousError
// arrives with the links package in Phase 3 and gets its own test there.
func TestErrorTypes(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("outer: %w", err) }

	var pe *PathError
	if !errors.As(wrap(&PathError{Op: "read", Path: "x", Reason: "r"}), &pe) || pe.Path != "x" {
		t.Error("V-ERR-01 PathError not matched by errors.As")
	}
	var ce *ConflictError
	err := wrap(&ConflictError{Path: "n.md", Expected: "aa", Current: "bb"})
	if !errors.As(err, &ce) || ce.Current != "bb" {
		t.Error("V-ERR-01 ConflictError not matched or Current lost")
	}
	if !strings.Contains(err.Error(), "re-read") {
		t.Errorf("V-WRITE-01 conflict message lacks a re-read hint: %v", err)
	}
	var nf *NotFoundError
	if !errors.As(wrap(&NotFoundError{Path: "gone.md"}), &nf) {
		t.Error("V-ERR-01 NotFoundError not matched by errors.As")
	}
}
