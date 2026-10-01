// Package appconfig persists sshush's own settings (distinct from the user's SSH
// config) at $XDG_CONFIG_HOME/sshush/config.toml, falling back to
// ~/.config/sshush/config.toml.
//
// Schema stability: the keys below are the frozen config.toml schema as of the
// v0.9.0 release candidate. Within the v1.x line, keys are never removed or
// repurposed — only added. Unknown keys are ignored (forward-compatible) and
// surfaced via Warnings() so a typo or a stale key is visible without being
// fatal.
package appconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/s-johri/sshush/internal/config"
	"github.com/s-johri/sshush/internal/fsutil"
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

	// Motion controls the opt-in animation/juice system.
	Motion MotionConfig `toml:"motion"`

	// ThemeName selects a built-in color theme (empty = default).
	ThemeName string `toml:"theme,omitempty"`

	// CheckUpdates toggles the async update-check on launch. A pointer so an
	// unset value defaults to on; set `check_updates = false` to disable.
	CheckUpdates *bool `toml:"check_updates,omitempty"`
}

// MotionConfig is the [motion] table: off by default.
type MotionConfig struct {
	Enabled   bool   `toml:"enabled"`
	Intensity string `toml:"intensity"` // subtle | normal | arcade
}

// Store reads and writes the settings file.
type Store struct {
	Path     string // settings file path; empty resolves to the default location
	cfg      Config
	warnings []string // non-fatal load warnings (e.g. unknown keys)
	// raw is the whole file as last loaded, unknown keys included, so a save
	// can write them back.
	raw map[string]any
	// loadErr is set when the file exists but did not load. Then sshush runs
	// with defaults, and save refuses to write them over the user's file.
	loadErr error
}

// New returns a Store for path. Empty path uses the default XDG location.
func New(path string) *Store { return &Store{Path: path} }

// Load reads the settings file into the store. A missing file is not an error;
// it yields zero-value settings. Unknown keys are ignored (forward-compatible)
// and recorded in Warnings().
func (s *Store) Load() (Config, error) {
	s.warnings = nil
	s.raw = nil
	s.loadErr = nil
	path, err := s.resolve()
	if err != nil {
		s.loadErr = err
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cfg = Config{}
			return s.cfg, nil
		}
		s.loadErr = err
		return Config{}, err
	}
	var cfg Config
	md, err := toml.Decode(string(data), &cfg)
	if err == nil {
		_, err = toml.Decode(string(data), &s.raw)
	}
	if err != nil {
		s.loadErr = err
		return Config{}, err
	}
	// Surface keys present in the file but not in the schema. The legacy
	// default_identity is part of the struct, so it never shows up here.
	for _, k := range md.Undecoded() {
		s.warnings = append(s.warnings, fmt.Sprintf("unknown setting %q (ignored)", k.String()))
	}
	// Migrate the legacy single default into the list.
	if len(cfg.DefaultIdentities) == 0 && cfg.DefaultIdentity != "" {
		cfg.DefaultIdentities = []string{cfg.DefaultIdentity}
	}
	cfg.DefaultIdentity = ""
	s.cfg = cfg
	return cfg, nil
}

// LoadErr returns why the last Load failed, or nil. A missing file is not a
// failure. While it is set, every change is refused, so the defaults that
// sshush runs with never replace the user's file.
func (s *Store) LoadErr() error { return s.loadErr }

// Warnings returns non-fatal issues from the last Load (e.g. unknown keys). The
// caller decides how to surface them; sshush prints them to stderr at startup.
func (s *Store) Warnings() []string { return s.warnings }

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

// MotionEnabled reports whether the motion/animation system is on (off default).
func (s *Store) MotionEnabled() bool { return s.cfg.Motion.Enabled }

// MotionIntensity is the motion level: subtle | normal | arcade (default normal).
func (s *Store) MotionIntensity() string {
	if s.cfg.Motion.Intensity == "" {
		return "normal"
	}
	return s.cfg.Motion.Intensity
}

// SetMotion enables/disables motion at a given intensity and persists.
func (s *Store) SetMotion(enabled bool, intensity string) error {
	s.cfg.Motion.Enabled = enabled
	if intensity != "" {
		s.cfg.Motion.Intensity = intensity
	}
	return s.save()
}

// CheckUpdates reports whether the launch update-check is enabled (default on).
func (s *Store) CheckUpdates() bool {
	if s.cfg.CheckUpdates != nil {
		return *s.cfg.CheckUpdates
	}
	return true
}

// ThemeName returns the configured theme name (empty = default).
func (s *Store) ThemeName() string { return s.cfg.ThemeName }

// SetTheme persists the selected theme name.
func (s *Store) SetTheme(name string) error {
	s.cfg.ThemeName = name
	return s.save()
}

// save writes the current settings, creating the parent directory as needed.
// It keeps keys that this version does not know, and writes through a temp
// file and a rename, so a crash never leaves a half-written file. It refuses
// to write after a failed Load.
func (s *Store) save() error {
	path, err := s.resolve()
	if err != nil {
		return err
	}
	if s.loadErr != nil {
		return fmt.Errorf("not saved: %s did not load (%v); fix or remove it", path, s.loadErr)
	}
	data, err := s.encode()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFile(path, data, 0o644) // keeps a symlink and the mode
}

// encode returns the file content: the raw document from the last Load with
// the known keys replaced by the current settings.
func (s *Store) encode() ([]byte, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(s.cfg); err != nil {
		return nil, err
	}
	var known map[string]any
	if _, err := toml.Decode(buf.String(), &known); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for k, v := range s.raw {
		out[k] = v
	}
	for _, k := range knownKeys {
		old, isTable := out[k].(map[string]any)
		cur, curTable := known[k].(map[string]any)
		delete(out, k)
		switch {
		case isTable && curTable: // keep unknown keys inside a known table
			merged := map[string]any{}
			for kk, vv := range old {
				merged[kk] = vv
			}
			for kk, vv := range cur {
				merged[kk] = vv
			}
			out[k] = merged
		case known[k] != nil:
			out[k] = known[k]
		}
	}
	buf.Reset()
	if err := toml.NewEncoder(&buf).Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// knownKeys are the top-level keys of Config, from its toml tags.
var knownKeys = func() []string {
	var out []string
	t := reflect.TypeOf(Config{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("toml"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}()

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
