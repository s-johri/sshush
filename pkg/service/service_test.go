package service

import (
	"errors"
	"testing"
)

// compile-time: App satisfies Service.
var _ Service = (*App)(nil)

func TestRefreshStubbed(t *testing.T) {
	a := New(nil, nil, nil)
	if _, err := a.Refresh(); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
}
