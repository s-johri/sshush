// Package appconfig persists sshush's own settings (distinct from the user's SSH
// config) at $XDG_CONFIG_HOME/sshush/config.toml, falling back to
// ~/.config/sshush/config.toml. It currently holds the default identity to load
// into the agent on startup.
package appconfig

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/s-johri/sshush/pkg/config"
)

// Config is the on-disk settings document.
type Config struct {
	// DefaultIdentities are the key names (Identity.Name) loaded into the agent
	// on startup. A non-empty list means auto-load is on.
	DefaultIdentities []string `toml:"default_identities"`

	// DefaultIdentity is the legacy single default; migrated into
	// DefaultIdentities on load and then dropped from the file.
	DefaultIdentity string `toml:"default_identity,omitempty"`

	// SshDir overrides the directory scanned for keys (default ~/.ssh). Relative
	// Include directives and ~ in the config resolve against it.
	SshDir string `toml:"ssh_dir,omitempty"`
	// ConfigPath overrides the SSH config file (default <ssh_dir>/config).
	ConfigPath string `toml:"config_path,omitempty"`
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
	// Migrate the legacy single default into the list.
	if len(cfg.DefaultIdentities) == 0 && cfg.DefaultIdentity != "" {
		cfg.DefaultIdentities = []string{cfg.DefaultIdentity}
	}
	cfg.DefaultIdentity = ""
	s.cfg = cfg
	return cfg, nil
}

// DefaultIdentities returns the configured default key names.
func (s *Store) DefaultIdentities() []config.IdentityID {
	out := make([]config.IdentityID, 0, len(s.cfg.DefaultIdentities))
	for _, n := range s.cfg.DefaultIdentities {
		out = append(out, config.IdentityID(n))
	}
	return out
}

// IsDefault reports whether id is one of the default identities.
func (s *Store) IsDefault(id config.IdentityID) bool {
	for _, n := range s.cfg.DefaultIdentities {
		if n == string(id) {
			return true
		}
	}
	return false
}

// AutoLoad reports whether any default identities are set.
func (s *Store) AutoLoad() bool { return len(s.cfg.DefaultIdentities) > 0 }

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

// ToggleDefault adds id to the defaults if absent, or removes it if present,
// then persists. Returns whether id is now a default.
func (s *Store) ToggleDefault(id config.IdentityID) (bool, error) {
	name := string(id)
	for i, n := range s.cfg.DefaultIdentities {
		if n == name {
			s.cfg.DefaultIdentities = append(s.cfg.DefaultIdentities[:i], s.cfg.DefaultIdentities[i+1:]...)
			return false, s.save()
		}
	}
	s.cfg.DefaultIdentities = append(s.cfg.DefaultIdentities, name)
	sort.Strings(s.cfg.DefaultIdentities)
	return true, s.save()
}

// ClearDefaults removes all default identities.
func (s *Store) ClearDefaults() error {
	s.cfg.DefaultIdentities = nil
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
