package appconfig

import (
	"os"
	"path/filepath"
	"strings"
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
	if len(cfg.DefaultIdentities) != 0 {
		t.Errorf("missing file should yield zero config: %+v", cfg)
	}
}

func TestToggleDefaultsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	s := New(path)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if added, err := s.ToggleDefault("id_ed"); err != nil || !added {
		t.Fatalf("add id_ed: added=%v err=%v", added, err)
	}
	if added, _ := s.ToggleDefault("id_rsa"); !added {
		t.Fatal("add id_rsa should report added")
	}
	if !s.IsDefault("id_ed") || !s.AutoLoad() || len(s.DefaultIdentities()) != 2 {
		t.Errorf("state wrong: %v", s.DefaultIdentities())
	}

	// Persisted across a fresh store.
	s2 := New(path)
	if _, err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if !s2.IsDefault("id_ed") || !s2.IsDefault("id_rsa") {
		t.Errorf("not persisted: %v", s2.DefaultIdentities())
	}

	// Toggling off removes it.
	if added, _ := s2.ToggleDefault("id_ed"); added {
		t.Error("toggling existing should remove (added=false)")
	}
	if s2.IsDefault("id_ed") {
		t.Error("id_ed should be removed")
	}
}

// TestLegacyMigration: an old config.toml with a single default_identity is read
// into the list and the legacy key is dropped on the next save.
func TestLegacyMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("default_identity = \"id_ed25519\"\nauto_load = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if !s.IsDefault("id_ed25519") || len(s.DefaultIdentities()) != 1 {
		t.Errorf("legacy default not migrated: %v", s.DefaultIdentities())
	}
	// Persist + reload: now stored as a list, legacy key gone.
	if _, err := s.ToggleDefault("id_rsa"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "default_identity ") {
		t.Errorf("legacy default_identity should be dropped after save:\n%s", data)
	}
}

func TestCheckUpdatesDefaultAndOverride(t *testing.T) {
	// Unset → defaults to on.
	if !New("").CheckUpdates() {
		t.Error("CheckUpdates should default to true when unset")
	}
	// check_updates = false from disk disables it.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("check_updates = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if s.CheckUpdates() {
		t.Error("check_updates = false should disable the update check")
	}
}

func TestUnknownKeysWarn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "theme = \"nord\"\nbogus_key = 1\n[motion]\nenabled = true\nwat = \"x\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	// Known keys parse; unknown ones are surfaced as warnings, not errors.
	if s.ThemeName() != "nord" || !s.MotionEnabled() {
		t.Errorf("known keys not parsed: theme=%q motion=%v", s.ThemeName(), s.MotionEnabled())
	}
	w := strings.Join(s.Warnings(), "\n")
	if !strings.Contains(w, "bogus_key") || !strings.Contains(w, "motion.wat") {
		t.Errorf("unknown keys not warned: %q", s.Warnings())
	}
	// A clean reload clears warnings.
	clean := filepath.Join(dir, "clean.toml")
	if err := os.WriteFile(clean, []byte("theme = \"nord\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s2 := New(clean)
	if _, err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if len(s2.Warnings()) != 0 {
		t.Errorf("clean config should have no warnings: %v", s2.Warnings())
	}
}

func TestMalformedConfigReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// Invalid TOML (unterminated string) must surface as an error, not be
	// silently swallowed — the caller decides to warn and fall back to defaults.
	if err := os.WriteFile(path, []byte("theme = \"nord\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path).Load(); err == nil {
		t.Error("malformed config.toml should return an error from Load")
	}

	// A missing file is NOT an error (zero-value settings).
	if _, err := New(filepath.Join(dir, "nope.toml")).Load(); err != nil {
		t.Errorf("missing config should not error: %v", err)
	}
}
