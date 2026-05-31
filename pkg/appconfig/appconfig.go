// Package appconfig persists sshush's own settings (distinct from the user's SSH
// config) at $XDG_CONFIG_HOME/sshush/config.toml, falling back to
// ~/.config/sshush/config.toml. It currently holds the default identity to load
// into the agent on startup.
package appconfig

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/s-johri/sshush/pkg/config"
)

// Config is the on-disk settings document.
type Config struct {
	// DefaultIdentity is the key name (Identity.Name) to load into the agent.
	DefaultIdentity string `toml:"default_identity"`
	// AutoLoad controls whether DefaultIdentity is loaded on startup.
	AutoLoad bool `toml:"auto_load"`

	// SshDir overrides the directory scanned for keys (default ~/.ssh). Relative
	// Include directives and ~ in the config resolve against it.
	SshDir string `toml:"ssh_dir"`
	// ConfigPath overrides the SSH config file (default <ssh_dir>/config).
	ConfigPath string `toml:"config_path"`
}

// Store reads and writes the settings file.
type Store struct {
	Path string // settings file path; empty resolves to the default location
	cfg  Config
}

// New returns a Store for path. Empty path uses the default XDG location.
func New(path string) *Store { return &Store{Path: path} }

// Load reads the settings file into the store. A missing file is not an error;
// it yields zero-value settings.
func (s *Store) Load() (Config, error) {
	path, err := s.resolve()
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			s.cfg = Config{}
			return s.cfg, nil
		}
		return Config{}, err
	}
	s.cfg = cfg
	return cfg, nil
}

// DefaultIdentity returns the configured default key name, if any.
func (s *Store) DefaultIdentity() config.IdentityID {
	return config.IdentityID(s.cfg.DefaultIdentity)
}

// AutoLoad reports whether the default identity should load on startup.
func (s *Store) AutoLoad() bool { return s.cfg.AutoLoad }

// SshDir returns the configured SSH directory, or "" to use the default (~/.ssh).
// Precedence: $SSHUSH_SSH_DIR > config file > default. ~ is expanded.
func (s *Store) SshDir() string {
	if v := os.Getenv("SSHUSH_SSH_DIR"); v != "" {
		return expandHome(v)
	}
	return expandHome(s.cfg.SshDir)
}

// ConfigPath returns the configured SSH config file, or "" for the default
// (<SshDir>/config, itself defaulting to ~/.ssh/config). Precedence:
// $SSHUSH_CONFIG > config file > <SshDir>/config when only SshDir is set.
func (s *Store) ConfigPath() string {
	if v := os.Getenv("SSHUSH_CONFIG"); v != "" {
		return expandHome(v)
	}
	if s.cfg.ConfigPath != "" {
		return expandHome(s.cfg.ConfigPath)
	}
	if dir := s.SshDir(); dir != "" {
		return filepath.Join(dir, "config")
	}
	return ""
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
		}
	}
	return p
}

// SetDefaultIdentity sets the default key (enabling auto-load) and persists it.
func (s *Store) SetDefaultIdentity(id config.IdentityID) error {
	s.cfg.DefaultIdentity = string(id)
	s.cfg.AutoLoad = true
	return s.save()
}

// ClearDefault removes the default identity and disables auto-load.
func (s *Store) ClearDefault() error {
	s.cfg.DefaultIdentity = ""
	s.cfg.AutoLoad = false
	return s.save()
}

// save writes the current settings, creating the parent directory as needed.
func (s *Store) save() error {
	path, err := s.resolve()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(s.cfg)
}

// resolve returns s.Path or the default settings path.
func (s *Store) resolve() (string, error) {
	if s.Path != "" {
		return s.Path, nil
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "sshush", "config.toml"), nil
}
