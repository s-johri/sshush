package clip

import (
	"errors"
	"testing"
)

func TestWriteUsesOverride(t *testing.T) {
	var got string
	restore := SetWriter(func(s string) error { got = s; return nil })
	defer restore()

	if err := Write("hello"); err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Errorf("override not used: %q", got)
	}
}

func TestWritePropagatesError(t *testing.T) {
	restore := SetWriter(func(string) error { return errors.New("no clipboard") })
	defer restore()
	if err := Write("x"); err == nil {
		t.Error("expected error from backend")
	}
}

func TestRestoreResetsBackend(t *testing.T) {
	restore := SetWriter(func(string) error { return nil })
	restore()
	// After restore, write should be the real backend again (non-nil).
	if write == nil {
		t.Error("restore left a nil backend")
	}
}
