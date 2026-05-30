package appconfig

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingIsZero(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "nope.toml"))
	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.DefaultIdentity != "" || cfg.AutoLoad {
		t.Errorf("missing file should yield zero config: %+v", cfg)
	}
}

func TestSetDefaultPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	s := New(path)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefaultIdentity("id_ed25519"); err != nil {
		t.Fatal(err)
	}
	if s.DefaultIdentity() != "id_ed25519" || !s.AutoLoad() {
		t.Errorf("in-memory state wrong: %q auto=%v", s.DefaultIdentity(), s.AutoLoad())
	}

	// A fresh store reading the same file sees the persisted values.
	s2 := New(path)
	if _, err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if s2.DefaultIdentity() != "id_ed25519" || !s2.AutoLoad() {
		t.Errorf("not persisted: %q auto=%v", s2.DefaultIdentity(), s2.AutoLoad())
	}
}
