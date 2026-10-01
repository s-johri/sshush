package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/s-johri/sshush/internal/sshconfig"
)

// TestConcurrentRefreshAndEdit guards T6: the TUI runs Refresh (hot reload,
// `r`) and edits in separate goroutines. App must serialize them, or an edit
// can be lost while it reports success, or the process can crash with
// "concurrent map writes". Run with -race.
func TestConcurrentRefreshAndEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("Host web\n    User u0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := sshconfig.New(path)
	repo.SshDir = dir
	repo.BackupDir = t.TempDir()
	a := New(fakeScanner{}, repo, &fakeAgent{})
	if _, err := a.Refresh(); err != nil {
		t.Fatal(err)
	}

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if _, err := a.Refresh(); err != nil {
				errs <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 1; i <= n; i++ {
			if err := a.EditHost("web", "User", fmt.Sprintf("u%d", i)); err != nil {
				errs <- err
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// The last edit must be on disk and in the model.
	want := fmt.Sprintf("u%d", n)
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "User "+want) {
		t.Errorf("file = %q, want User %s", got, want)
	}
	model, err := a.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if u := model.Hosts["web"].User; u != want {
		t.Errorf("model User = %q, want %q", u, want)
	}
}
