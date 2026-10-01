package service

import (
	"os"
	"path/filepath"
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
