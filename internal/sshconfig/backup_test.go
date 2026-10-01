package sshconfig

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if got := backupFiles(r); len(got) != 1 || got[0] != path {
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

// TestBackupsReportPathAndTime: each backup gives the config file, the
// backup file and the backup's modification time, so the user can see how
// old the snapshot is before a restore.
func TestBackupsReportPathAndTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host web\n    User old\n")

	r := New(path)
	r.BackupDir = t.TempDir()
	editAndSave(t, r, "web", "User", "new")
	when := time.Date(2026, 9, 1, 10, 30, 0, 0, time.Local)
	if err := os.Chtimes(r.backupPath(path), when, when); err != nil {
		t.Fatal(err)
	}

	got := r.Backups()
	if len(got) != 1 {
		t.Fatalf("Backups() = %v, want 1", got)
	}
	b := got[0]
	if b.File != path || b.Path != r.backupPath(path) || !b.ModTime.Equal(when) {
		t.Errorf("Backups()[0] = %+v, want file %s, path %s, time %v", b, path, r.backupPath(path), when)
	}
	if want := r.backupPath(path) + ".pre-restore"; b.PreRestore != want {
		t.Errorf("PreRestore = %q, want %q", b.PreRestore, want)
	}
}

// TestRestoreKeepsPreRestoreCopy: Restore saves the current content before
// it overwrites the file, so a restore can be undone.
func TestRestoreKeepsPreRestoreCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host web\n    User old\n")

	r := New(path)
	r.BackupDir = t.TempDir()
	editAndSave(t, r, "web", "User", "new")
	if _, err := r.Restore(); err != nil {
		t.Fatal(err)
	}
	pre, err := os.ReadFile(r.backupPath(path) + ".pre-restore")
	if err != nil {
		t.Fatalf("no pre-restore copy: %v", err)
	}
	if want := "Host web\n    User new\n"; string(pre) != want {
		t.Errorf("pre-restore = %q, want %q", pre, want)
	}
}

// TestRestoreLegacyKeepsPreRestoreInStateDir: for a sibling .bak from an
// earlier version, the pre-restore copy also goes to the state dir, not next
// to the file, where an Include glob could match it.
func TestRestoreLegacyKeepsPreRestoreInStateDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host web\n    User new\n")
	writeFile(t, path+".bak", "Host web\n    User old\n")

	r := New(path)
	r.BackupDir = t.TempDir()
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Restore(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.backupPath(path) + ".pre-restore"); err != nil {
		t.Errorf("pre-restore not in state dir: %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.pre-restore")); len(m) != 0 {
		t.Errorf("pre-restore written next to the file: %v", m)
	}
}

// backupFiles returns the config files that have a backup.
func backupFiles(r *FileRepo) []string {
	var out []string
	for _, b := range r.Backups() {
		out = append(out, b.File)
	}
	return out
}

// TestSaveAndRestoreThroughSymlink guards T18: with ~/.ssh/config linked to
// a dotfiles file, a save and a restore write the target and keep the link.
func TestSaveAndRestoreThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "ssh_config")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	orig := "Host a\n    User old\n"
	writeFile(t, target, orig)
	link := filepath.Join(dir, "config")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	isLink := func() bool {
		fi, err := os.Lstat(link)
		return err == nil && fi.Mode()&os.ModeSymlink != 0
	}

	r := New(link)
	r.BackupDir = t.TempDir()
	editAndSave(t, r, "a", "User", "new")
	if !isLink() {
		t.Fatal("save replaced the link")
	}
	if got, _ := os.ReadFile(target); string(got) != "Host a\n    User new\n" {
		t.Errorf("target after save = %q", got)
	}
	if _, err := r.Restore(); err != nil {
		t.Fatal(err)
	}
	if !isLink() {
		t.Fatal("restore replaced the link")
	}
	if got, _ := os.ReadFile(target); string(got) != orig {
		t.Errorf("target after restore = %q", got)
	}
}
