package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/s-johri/sshush/internal/config"
	"github.com/s-johri/sshush/internal/sshconfig"
)

// TestRefreshWorksWhenConfigDoesNotLoad guards T10: keys and the agent must
// work when the SSH config does not load, so `sshush load-default` (run from
// every new shell) keeps working.
func TestRefreshWorksWhenConfigDoesNotLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("Host a\n"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("file is still readable (running as root?)")
	}
	repo := sshconfig.New(path)
	repo.SshDir = dir
	keyPath := filepath.Join(dir, "id_ed25519")
	scanner := fakeScanner{ids: []config.Identity{{
		ID: "id_ed25519", Name: "id_ed25519", Path: keyPath, ExistsOnDisk: true,
	}}}
	ag := &fakeAgent{}
	a := New(scanner, repo, ag)

	model, err := a.Refresh()
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if model.ConfigErr == "" {
		t.Error("ConfigErr is empty")
	}
	if _, ok := model.Identities["id_ed25519"]; !ok {
		t.Fatalf("keys missing: %v", model.Identities)
	}
	if err := a.AddKeyToAgent("id_ed25519"); err != nil {
		t.Fatalf("AddKeyToAgent: %v", err)
	}
	if len(ag.added) != 1 || ag.added[0] != keyPath {
		t.Errorf("agent added %v, want [%s]", ag.added, keyPath)
	}
	if err := a.EditHost("a", "User", "x"); err == nil {
		t.Error("EditHost worked with a config that did not load")
	}
}
