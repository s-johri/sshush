package sshconfig

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s-johri/sshush/internal/config"
)

// TestMain points XDG_STATE_HOME at a temp dir, so no test in this package
// writes backups into the real ~/.local/state.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sshush-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// editAndSave loads r, sets key=val on host h, then saves.
func editAndSave(t *testing.T, r *FileRepo, h, key, val string) {
	t.Helper()
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHostField(config.HostID(h), key, val); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
}

// TestBackupGoesToStateDir: the backup must not go next to the config file,
// because an Include glob can match it (C1).
func TestBackupGoesToStateDir(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	orig := "Host web\n    User old\n"
	writeFile(t, path, orig)

	editAndSave(t, New(path), "web", "User", "new")

	want := filepath.Join(state, "sshush", "backups", url.PathEscape(path)+".bak")
	bak, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("backup not in state dir: %v", err)
	}
	if string(bak) != orig {
		t.Errorf("backup = %q, want %q", bak, orig)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("backup written next to the config file: %v", err)
	}
	fi, err := os.Stat(filepath.Dir(want))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("backup dir mode = %o, want 700", fi.Mode().Perm())
	}
}

// TestBackupDirFallsBackToLocalState: with no XDG_STATE_HOME, backups go to
// ~/.local/state/sshush/backups.
func TestBackupDirFallsBackToLocalState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	want := filepath.Join(home, ".local", "state", "sshush", "backups")
	if got := DefaultBackupDir(); got != want {
		t.Errorf("DefaultBackupDir() = %q, want %q", got, want)
	}
}

// TestIncludeGlobEditShowsNewValue reproduces C1: with Include config.d/*,
// an edit of config.d/work must not leave a file that the glob loads with
// the old value.
func TestIncludeGlobEditShowsNewValue(t *testing.T) {
	dir := t.TempDir()
	confd := filepath.Join(dir, "config.d")
	if err := os.Mkdir(confd, 0o700); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "config")
	writeFile(t, main, "Include config.d/*\n")
	writeFile(t, filepath.Join(confd, "work"), "Host work\n    User old\n")

	r := New(main)
	r.SshDir = dir
	editAndSave(t, r, "work", "User", "new")

	entries, _ := os.ReadDir(confd)
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("config.d has %v, want only [work]", names)
	}
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := model.Hosts["work"].User; got != "new" {
		t.Errorf("User = %q after save, want %q", got, "new")
	}
}

// TestIncludeGlobSkipsLegacyBak: an old sibling .bak (from sshush before
// 1.0) that an Include glob matches is not loaded, and the model warns the
// user, because ssh still reads it.
func TestIncludeGlobSkipsLegacyBak(t *testing.T) {
	dir := t.TempDir()
	confd := filepath.Join(dir, "config.d")
	if err := os.Mkdir(confd, 0o700); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "config")
	writeFile(t, main, "Include config.d/*\n")
	writeFile(t, filepath.Join(confd, "work"), "Host work\n    User new\n")
	stale := filepath.Join(confd, "work.bak")
	writeFile(t, stale, "Host work\n    User old\n")

	r := New(main)
	r.SshDir = dir
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := model.Hosts["work"].User; got != "new" {
		t.Errorf("User = %q, want %q (the .bak was loaded)", got, "new")
	}
	for _, f := range model.SourceFiles {
		if strings.HasSuffix(f, ".bak") {
			t.Errorf("SourceFiles has %s", f)
		}
	}
	if len(model.Warnings) != 1 || !strings.Contains(model.Warnings[0], stale) {
		t.Errorf("Warnings = %q, want one that names %s", model.Warnings, stale)
	}
}

// TestRestoreFindsLegacyBak: a sibling .bak from sshush before 1.0 can still
// be restored.
func TestRestoreFindsLegacyBak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host web\n    User new\n")
	orig := "Host web\n    User old\n"
	writeFile(t, path+".bak", orig)

	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if got := r.BackupPaths(); len(got) != 1 || got[0] != path {
		t.Fatalf("BackupPaths() = %v, want [%s]", got, path)
	}
	if _, err := r.Restore(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != orig {
		t.Errorf("not restored from legacy .bak: %q", got)
	}
}

// TestRestorePrefersStateDirBackup: when both exist, the backup in the state
// dir is newer, so Restore uses it.
func TestRestorePrefersStateDirBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	orig := "Host web\n    User old\n"
	writeFile(t, path, orig)
	writeFile(t, path+".bak", "Host web\n    User legacy\n")

	r := New(path)
	editAndSave(t, r, "web", "User", "new")
	if _, err := r.Restore(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != orig {
		t.Errorf("restored %q, want %q", got, orig)
	}
}
