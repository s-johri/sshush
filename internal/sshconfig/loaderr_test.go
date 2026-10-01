package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s-johri/sshush/internal/config"
)

// unreadable writes content to path and removes read access. It skips the
// test when the OS still lets the file be read (for example as root).
func unreadable(t *testing.T, path, content string) {
	t.Helper()
	writeFile(t, path, content)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("file is still readable (running as root?)")
	}
}

// TestConfigLoadErrorGivesPartialModel guards T10: a config that does not
// load must not stop sshush. Load returns what it could read and the error
// in the model, and refuses every change.
func TestConfigLoadErrorGivesPartialModel(t *testing.T) {
	dir := t.TempDir()
	inc := filepath.Join(dir, "inc.conf")
	unreadable(t, inc, "Host hidden\n    User h\n")
	main := filepath.Join(dir, "config")
	orig := "Host a\n    User u\n\nInclude " + inc + "\n"
	writeFile(t, main, orig)

	r := New(main)
	model, err := r.Load()
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	if !strings.Contains(model.ConfigErr, inc) {
		t.Errorf("ConfigErr = %q, want one that names %s", model.ConfigErr, inc)
	}
	if model.Hosts["a"].User != "u" {
		t.Errorf("host a not loaded: %v", hostIDs(model))
	}

	if err := r.SetHostField("a", "User", "x"); err == nil {
		t.Error("SetHostField worked after a failed load")
	}
	if err := r.AddHost(config.Host{ID: "n", Name: "n"}); err == nil {
		t.Error("AddHost worked after a failed load")
	}
	if err := r.DeleteHost("a"); err == nil {
		t.Error("DeleteHost worked after a failed load")
	}
	if err := r.Save(); err == nil {
		t.Error("Save worked after a failed load")
	}
	if got, _ := os.ReadFile(main); string(got) != orig {
		t.Errorf("main config changed:\n%s", got)
	}
}

// TestConfigLoadErrorStillRestores: restore is what a user needs when the
// config is broken, so it must still work.
func TestConfigLoadErrorStillRestores(t *testing.T) {
	dir := t.TempDir()
	inc := filepath.Join(dir, "inc.conf")
	unreadable(t, inc, "Host hidden\n")
	main := filepath.Join(dir, "config")
	writeFile(t, main, "Include "+inc+"\nHost a\n    User broken\n")
	writeFile(t, main+".bak", "Host a\n    User good\n") // a legacy backup

	r := New(main)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got, _ := os.ReadFile(main); string(got) != "Host a\n    User good\n" {
		t.Errorf("not restored: %q", got)
	}
}

// TestConfigUnreadableMainFile: the main file itself does not load.
func TestConfigUnreadableMainFile(t *testing.T) {
	main := filepath.Join(t.TempDir(), "config")
	unreadable(t, main, "Host a\n")
	model, err := New(main).Load()
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	if model.ConfigErr == "" || len(model.Hosts) != 0 {
		t.Errorf("ConfigErr = %q, hosts = %v", model.ConfigErr, hostIDs(model))
	}
}
