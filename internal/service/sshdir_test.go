package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s-johri/sshush/internal/config"
	"github.com/s-johri/sshush/internal/perms"
)

// TestSshDirNotGuessedFromConfigPath guards T8: with config_path in a
// dotfiles dir, the audit and known_hosts must use the configured SSH dir,
// not the directory of the config file.
func TestSshDirNotGuessedFromConfigPath(t *testing.T) {
	root := t.TempDir()
	sshDir := filepath.Join(root, "ssh")
	dotfiles := filepath.Join(root, "dotfiles")
	for _, d := range []string{sshDir, dotfiles} {
		if err := os.Mkdir(d, 0o755); err != nil { // 0755: the audit flags a dir
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"),
		[]byte("right.example ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dotfiles, "known_hosts"),
		[]byte("wrong.example ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &fakeConfig{model: &config.SshConfigModel{
		SourceFiles: []string{filepath.Join(dotfiles, "ssh_config")},
	}}
	a := New(fakeScanner{}, cfg, &fakeAgent{})
	a.SshDir = sshDir
	if _, err := a.Refresh(); err != nil {
		t.Fatal(err)
	}

	issues, err := a.AuditPermissions()
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, i := range issues {
		if i.Kind == perms.DirKind {
			dirs = append(dirs, i.Path)
		}
	}
	if len(dirs) != 1 || dirs[0] != sshDir {
		t.Errorf("audit dir issues = %v, want [%s] (never the dotfiles dir)", dirs, sshDir)
	}

	entries, err := a.KnownHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Hosts[0] != "right.example" {
		t.Errorf("known_hosts entries = %+v, want the one in %s", entries, sshDir)
	}
}

// TestSshDirDefaultsToHomeSsh: with no SshDir, the audit uses ~/.ssh, even
// when the config file is somewhere else.
func TestSshDirDefaultsToHomeSsh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &fakeConfig{model: &config.SshConfigModel{
		SourceFiles: []string{filepath.Join(home, "dotfiles", "ssh_config")},
	}}
	a := New(fakeScanner{}, cfg, &fakeAgent{})
	if _, err := a.Refresh(); err != nil {
		t.Fatal(err)
	}
	issues, err := a.AuditPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Path != sshDir {
		t.Errorf("issues = %+v, want one for %s", issues, sshDir)
	}
}

// TestRemoveKnownHostKeepsFirstBackup guards T13 through the service: App
// keeps one Remover for its lifetime, so two removals keep the first backup.
func TestRemoveKnownHostKeepsFirstBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"
	orig := "a.example " + key + "\nb.example " + key + "\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(fakeScanner{}, &fakeConfig{model: &config.SshConfigModel{}}, &fakeAgent{})
	a.SshDir = dir
	for i := 0; i < 2; i++ {
		entries, err := a.KnownHosts()
		if err != nil || len(entries) == 0 {
			t.Fatalf("KnownHosts: %v %v", entries, err)
		}
		if err := a.RemoveKnownHost(entries[0]); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(path + ".bak"); string(got) != orig {
		t.Errorf(".bak = %q, want the original", got)
	}
}

// TestFixPermissionsReportsPartialResult guards T16: one failed chmod must
// not stop the others, and the result must say which files were fixed.
func TestFixPermissionsReportsPartialResult(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "id_rsa")
	if err := os.WriteFile(good, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone")
	a := New(fakeScanner{}, &fakeConfig{model: &config.SshConfigModel{}}, &fakeAgent{})
	fixed, err := a.FixPermissions([]perms.Issue{
		{Path: missing, Want: 0o600},
		{Path: good, Want: 0o600},
	})
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("err = %v, want one that names %s", err, missing)
	}
	if len(fixed) != 1 || fixed[0].Path != good {
		t.Errorf("fixed = %v, want [%s]", fixed, good)
	}
	if fi, _ := os.Stat(good); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600 (the chmod after the failure did not run)", fi.Mode().Perm())
	}
}
