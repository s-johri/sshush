package sshconfig

import (
	"errors"
	"testing"
)

// compile-time: FileRepo satisfies ConfigRepo.
var _ ConfigRepo = (*FileRepo)(nil)

func TestLoadStubbed(t *testing.T) {
	if _, err := New("").Load(); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
}
