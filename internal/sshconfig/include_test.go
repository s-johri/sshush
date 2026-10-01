package sshconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/s-johri/sshush/internal/config"
)

// The tests in this file guard T19.

// TestRelativeIncludeUsesSshDir: a relative Include resolves against the
// SSH dir that sshush was given, not the real $HOME/.ssh.
func TestRelativeIncludeUsesSshDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".ssh", "extra.conf"), "Host wrong\n")

	sshDir := t.TempDir()
	writeFile(t, filepath.Join(sshDir, "extra.conf"), "Host right\n")
	main := filepath.Join(sshDir, "config")
	writeFile(t, main, "Include extra.conf\n")

	r := New(main)
	r.SshDir = sshDir
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := model.Hosts["right"]; !ok {
		t.Errorf("hosts = %v, want right (from SshDir)", hostIDs(model))
	}
	if _, ok := model.Hosts["wrong"]; ok {
		t.Error("loaded the Include from the real ~/.ssh")
	}
}

// TestQuotedIncludeWithSpaces: a quoted path is one path, also with spaces.
func TestQuotedIncludeWithSpaces(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.Join(dir, "My Configs")
	if err := os.Mkdir(spaced, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(spaced, "work"), "Host work\n")
	writeFile(t, filepath.Join(dir, "other"), "Host other\n")
	main := filepath.Join(dir, "config")
	writeFile(t, main, `Include "`+filepath.Join(spaced, "work")+`" `+filepath.Join(dir, "other")+"  # two files\n")

	r := New(main)
	r.SshDir = dir
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"work", "other"} {
		if _, ok := model.Hosts[config.HostID(h)]; !ok {
			t.Errorf("host %s not loaded; hosts = %v", h, hostIDs(model))
		}
	}
}
