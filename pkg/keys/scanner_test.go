package keys

import (
	"errors"
	"testing"
)

// compile-time: DiskScanner satisfies KeyScanner.
var _ KeyScanner = (*DiskScanner)(nil)

func TestScanStubbed(t *testing.T) {
	if _, err := New("").Scan(); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
}
