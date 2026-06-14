package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallExtrasFreshAndRefresh(t *testing.T) {
	data := t.TempDir()
	cfg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_CONFIG_HOME", cfg)

	// Fresh install writes all four assets.
	if err := installExtras(false); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(data, "man/man1/sshush.1"),
		filepath.Join(data, "bash-completion/completions/sshush"),
		filepath.Join(data, "zsh/site-functions/_sshush"),
		filepath.Join(cfg, "fish/completions/sshush.fish"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}

	// --refresh only overwrites existing files: remove one, refresh, still gone.
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0], []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installExtras(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths[1]); !os.IsNotExist(err) {
		t.Error("--refresh must not create files that were not installed")
	}
	if b, _ := os.ReadFile(paths[0]); string(b) == "stale" {
		t.Error("--refresh should overwrite existing files with current assets")
	}
}
