package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed sshush.1
var manPage []byte

// dataHome is ${XDG_DATA_HOME:-~/.local/share}.
func dataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".local/share"
	}
	return filepath.Join(home, ".local", "share")
}

// configHome is ${XDG_CONFIG_HOME:-~/.config}.
func configHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config"
	}
	return filepath.Join(home, ".config")
}

// installExtras writes the embedded man page and shell completions to
// user-level paths. With refresh, only files that already exist are
// overwritten (the opt-in refresh `sshush update` runs); a fresh install
// writes everything. Serves manual/install.sh installs — Homebrew and AUR
// package these files themselves.
func installExtras(refresh bool) error {
	read := func(name string) []byte {
		b, err := completionFS.ReadFile("completions/" + name)
		if err != nil {
			panic("embedded completion missing: " + name) // build-time invariant
		}
		return b
	}
	assets := []struct {
		path string
		data []byte
	}{
		{filepath.Join(dataHome(), "man", "man1", "sshush.1"), manPage},
		{filepath.Join(dataHome(), "bash-completion", "completions", "sshush"), read("sshush.bash")},
		{filepath.Join(dataHome(), "zsh", "site-functions", "_sshush"), read("sshush.zsh")},
		{filepath.Join(configHome(), "fish", "completions", "sshush.fish"), read("sshush.fish")},
	}
	var wrote []string
	for _, a := range assets {
		if refresh {
			if _, err := os.Stat(a.path); err != nil {
				continue // not previously installed: refresh leaves it alone
			}
		}
		if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(a.path, a.data, 0o644); err != nil {
			return err
		}
		wrote = append(wrote, a.path)
	}
	for _, p := range wrote {
		fmt.Println("installed", p)
	}
	if len(wrote) == 0 {
		fmt.Println("nothing to refresh (run `sshush install-extras` for a full install)")
		return nil
	}
	// zsh has no standard user completion dir; the hint is unconditional.
	fmt.Printf("zsh users: ensure fpath includes it, e.g.\n  fpath+=(%s)\n",
		filepath.Join(dataHome(), "zsh", "site-functions"))
	return nil
}
