package keys

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/s-johri/sshush/pkg/config"
)

func TestGenerateAndDelete(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	dir := t.TempDir()
	s := New(dir)

	id, err := s.Generate(GenerateOpts{
		Name: "id_test", Comment: "unit@test", Algorithm: config.AlgED25519,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := os.Stat(id.Path); err != nil {
		t.Errorf("private key missing: %v", err)
	}
	if _, err := os.Stat(id.PublicKeyPath); err != nil {
		t.Errorf("public key missing: %v", err)
	}

	// Scan should now discover it with a real fingerprint.
	ids, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0].Fingerprint == "" || ids[0].Algorithm != config.AlgED25519 {
		t.Errorf("scan after generate = %+v", ids)
	}

	// Refuse overwrite.
	if _, err := s.Generate(GenerateOpts{Name: "id_test", Algorithm: config.AlgED25519}); err == nil {
		t.Error("Generate should refuse to overwrite existing key")
	}

	if err := s.Delete(id.Path); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(id.Path); !os.IsNotExist(err) {
		t.Error("private key not removed")
	}
	if _, err := os.Stat(id.PublicKeyPath); !os.IsNotExist(err) {
		t.Error("public key not removed")
	}
}

func TestDeleteEmptyPathRefused(t *testing.T) {
	if err := New("").Delete(""); err == nil {
		t.Error("Delete of empty path should error")
	}
}

func TestGenerateCommandInteractive(t *testing.T) {
	cmd, priv, err := GenerateCommand(GenerateOpts{
		Dir: "/keys", Name: "id_ed25519", Algorithm: config.AlgED25519, Comment: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	if priv != filepath.Join("/keys", "id_ed25519") {
		t.Errorf("priv path = %q", priv)
	}
	// Interactive command must NOT pass -N (so ssh-keygen can prompt).
	for _, a := range cmd.Args {
		if a == "-N" {
			t.Error("interactive GenerateCommand must not include -N")
		}
	}
}
