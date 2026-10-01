package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchSignalsOnChange(t *testing.T) {
	w, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	dir := t.TempDir()
	if err := w.Watch([]string{dir}); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("Host x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case <-w.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no change signal within timeout")
	}
}

func TestWatchCoalesces(t *testing.T) {
	w, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	dir := t.TempDir()
	if err := w.Watch([]string{dir}); err != nil {
		t.Fatal(err)
	}

	// A burst of writes (like an editor save) should yield one signal.
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, "config"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-w.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("expected a coalesced signal")
	}
	// No second signal should arrive for the same burst.
	select {
	case <-w.Events():
		t.Error("burst produced more than one signal")
	case <-time.After(400 * time.Millisecond):
	}
}
