package knownhosts

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	edKey  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOnXKPflTu3pBUu9frCSu0vaVdCgwoHSe5LQcBTZGLFK"
	fpED   = "SHA256:WJ2qO5SZailDsTNYXJiD9YSPvHrUfDO+AfcDDmVgcfw"
	rsaKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCi+n7z0jPcq+uP53RiOWRYG37cJtKDlGviq7ISe7THLSvmyIbsk6rTyKg9rLqoMYuRAn8XWjScPZS2Bbk1D4Bm2se99gBn4SlB0YGJ9xDtfQxh4zBUD/jj2zMZXNCL6iT+Gt5fV2vuVxfg5x0UAlGvr04TGQWKjVsSrZGP7MRT7u0TG6la7jB/PqxcI7FI4LWvMiYAVuRbMC5t/o1BcfDh9zDhC/xR1dUkvsx9a3MsHDQGekDcqsYqW/fPh8L3OBfB14k5l1vxlN466ult0aXbQWyeuAtKOTALawaWsXMKjjmajejAa5Cf9yTJhDs6qy+mKXa97EmZn/auPWIbLGPH"
)

func TestParseAndRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	content := "# a comment\n" +
		"github.com " + edKey + "\n" +
		"\n" +
		"gitlab.com,gl " + rsaKey + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(entries), entries)
	}
	gh := entries[0]
	if gh.Hosts[0] != "github.com" || gh.KeyType != "ssh-ed25519" || gh.Fingerprint != fpED {
		t.Errorf("github entry wrong: %+v", gh)
	}
	if gh.Line != 1 { // line 0 is the comment
		t.Errorf("github line = %d, want 1", gh.Line)
	}
	gl := entries[1]
	if len(gl.Hosts) != 2 || gl.Line != 3 {
		t.Errorf("gitlab entry wrong: %+v", gl)
	}

	// Remove the github line; gitlab remains, backup written.
	var r Remover
	if err := r.Remove(path, gh); err != nil {
		t.Fatal(err)
	}
	after, _ := Parse(path)
	if len(after) != 1 || after[0].Hosts[0] != "gitlab.com" {
		t.Errorf("after remove: %+v", after)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("backup not written: %v", err)
	}
}

func TestParseMissingFile(t *testing.T) {
	got, err := Parse(filepath.Join(t.TempDir(), "nope"))
	if err != nil || got != nil {
		t.Errorf("missing file: got %v, err %v", got, err)
	}
}

// TestRemoveKeepsFirstBackup guards T13: the .bak is the file from before
// the first removal of the session. A second removal must not overwrite it.
func TestRemoveKeepsFirstBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	orig := "github.com " + edKey + "\ngitlab.com " + rsaKey + "\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	var r Remover
	for i := 0; i < 2; i++ {
		entries, err := Parse(path)
		if err != nil || len(entries) == 0 {
			t.Fatalf("Parse: %v %v", entries, err)
		}
		if err := r.Remove(path, entries[0]); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(path + ".bak"); string(got) != orig {
		t.Errorf(".bak = %q, want the original %q", got, orig)
	}
	if fi, _ := os.Stat(path + ".bak"); fi.Mode().Perm() != 0o600 {
		t.Errorf(".bak mode = %o, want 600", fi.Mode().Perm())
	}

	// A deleted .bak is written again on the next removal.
	if err := os.Remove(path + ".bak"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, _ := Parse(path)
	if err := r.Remove(path, entries[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf(".bak not written again: %v", err)
	}
}

// TestRemoveRefusesChangedLine guards T13: when the file changed after it
// was read (ssh added a host), the line index can point at another entry.
// Remove must refuse, not delete the wrong line.
func TestRemoveRefusesChangedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte("github.com "+edKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, _ := Parse(path)
	changed := "new.example " + rsaKey + "\ngithub.com " + edKey + "\n"
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	var r Remover
	if err := r.Remove(path, entries[0]); err == nil {
		t.Error("Remove deleted a line that changed")
	}
	if got, _ := os.ReadFile(path); string(got) != changed {
		t.Errorf("file changed: %q", got)
	}
}
