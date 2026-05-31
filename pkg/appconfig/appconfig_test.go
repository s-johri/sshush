package appconfig

import (
	"path/filepath"
	"testing"
)

func TestSshDirAndConfigPathResolution(t *testing.T) {
	t.Setenv("SSHUSH_SSH_DIR", "")
	t.Setenv("SSHUSH_CONFIG", "")

	// Only ssh_dir set => config path defaults to <ssh_dir>/config.
	s := New("")
	s.cfg.SshDir = "/custom/ssh"
	if got := s.SshDir(); got != "/custom/ssh" {
		t.Errorf("SshDir = %q", got)
	}
	if got := s.ConfigPath(); got != "/custom/ssh/config" {
		t.Errorf("ConfigPath = %q, want /custom/ssh/config", got)
	}

	// Explicit config_path wins over the ssh_dir-derived default.
	s.cfg.ConfigPath = "/elsewhere/cfg"
	if got := s.ConfigPath(); got != "/elsewhere/cfg" {
		t.Errorf("explicit ConfigPath = %q", got)
	}

	// Env overrides both.
	t.Setenv("SSHUSH_SSH_DIR", "/env/ssh")
	t.Setenv("SSHUSH_CONFIG", "/env/cfg")
	if got := s.SshDir(); got != "/env/ssh" {
		t.Errorf("env SshDir = %q", got)
	}
	if got := s.ConfigPath(); got != "/env/cfg" {
		t.Errorf("env ConfigPath = %q", got)
	}
}

func TestDefaultsEmptyWhenUnset(t *testing.T) {
	t.Setenv("SSHUSH_SSH_DIR", "")
	t.Setenv("SSHUSH_CONFIG", "")
	s := New("")
	if s.SshDir() != "" || s.ConfigPath() != "" {
		t.Errorf("unset should yield empty (defaults): dir=%q path=%q", s.SshDir(), s.ConfigPath())
	}
}

func TestLoadMissingIsZero(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "nope.toml"))
	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.DefaultIdentity != "" || cfg.AutoLoad {
		t.Errorf("missing file should yield zero config: %+v", cfg)
	}
}

func TestSetDefaultPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	s := New(path)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefaultIdentity("id_ed25519"); err != nil {
		t.Fatal(err)
	}
	if s.DefaultIdentity() != "id_ed25519" || !s.AutoLoad() {
		t.Errorf("in-memory state wrong: %q auto=%v", s.DefaultIdentity(), s.AutoLoad())
	}

	// A fresh store reading the same file sees the persisted values.
	s2 := New(path)
	if _, err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if s2.DefaultIdentity() != "id_ed25519" || !s2.AutoLoad() {
		t.Errorf("not persisted: %q auto=%v", s2.DefaultIdentity(), s2.AutoLoad())
	}
}
