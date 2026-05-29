package agent

import (
	"errors"
	"testing"
)

// compile-time: Client satisfies AgentClient.
var _ AgentClient = (*Client)(nil)

func TestListStubbed(t *testing.T) {
	if _, err := New("").List(); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
}
