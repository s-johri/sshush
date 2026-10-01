package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestSaveRefusedAfterFailedLoad guards T7: when config.toml does not load,
// sshush runs with defaults, and a later `t`, `s` or `m` must not write those
// defaults over the user's file.
func TestSaveRefusedAfterFailedLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	bad := "default_identities = [\"id_work\"]\ntheme = \"nord\n"
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if _, err := s.Load(); err == nil {
		t.Fatal("Load of bad TOML succeeded")
	}
	if s.LoadErr() == nil {
		t.Error("LoadErr() = nil after a failed Load")
	}
	if err := s.SetTheme("dracula"); err == nil {
		t.Error("SetTheme saved after a failed Load")
	}
	if _, err := s.ToggleDefault("id_x"); err == nil {
		t.Error("ToggleDefault saved after a failed Load")
	}
	if err := s.SetMotion(true, ""); err == nil {
		t.Error("SetMotion saved after a failed Load")
	}
	if got, _ := os.ReadFile(path); string(got) != bad {
		t.Errorf("file changed:\n%s", got)
	}
}

// TestSaveAfterMissingFileWorks: a missing file is not a failed load.
func TestSaveAfterMissingFileWorks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	s := New(path)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if s.LoadErr() != nil {
		t.Errorf("LoadErr() = %v for a missing file", s.LoadErr())
	}
	if err := s.SetTheme("nord"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), `theme = "nord"`) {
		t.Errorf("file = %q", got)
	}
}

// TestSaveKeepsUnknownKeys: keys from a newer sshush (or a typo the user
// still wants to fix) must survive a save, also inside [motion].
func TestSaveKeepsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	in := "theme = \"nord\"\nfuture_key = \"x\"\n\n[motion]\nenabled = false\nsparkle = true\n\n[future_table]\nn = 3\n"
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTheme(""); err != nil { // a cleared known key goes away
		t.Fatal(err)
	}
	if err := s.SetMotion(true, "arcade"); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatal(err)
	}
	if got["future_key"] != "x" {
		t.Errorf("future_key = %v, want x", got["future_key"])
	}
	if _, ok := got["theme"]; ok {
		t.Errorf("theme = %v, want it removed", got["theme"])
	}
	motion, _ := got["motion"].(map[string]any)
	if motion["sparkle"] != true || motion["enabled"] != true || motion["intensity"] != "arcade" {
		t.Errorf("motion = %v", motion)
	}
	ft, _ := got["future_table"].(map[string]any)
	if ft["n"] != int64(3) {
		t.Errorf("future_table = %v", ft)
	}
}

// TestSaveKeepsModeAndSymlink: the temp-file-and-rename write must keep the
// file mode, and must write through a symlink (a dotfiles setup), not
// replace it.
func TestSaveKeepsModeAndSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "sshush.toml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("theme = \"nord\"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	s := New(link)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTheme("dracula"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("config.toml is no longer a symlink")
	}
	got, _ := os.ReadFile(target)
	if !strings.Contains(string(got), `theme = "dracula"`) {
		t.Errorf("target = %q", got)
	}
	if ti, _ := os.Stat(target); ti.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 640", ti.Mode().Perm())
	}
	for _, d := range []string{dir, filepath.Dir(target)} {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp") {
				t.Errorf("temp file left: %s", filepath.Join(d, e.Name()))
			}
		}
	}
}
