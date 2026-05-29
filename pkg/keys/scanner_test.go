package keys

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/s-johri/sshush/pkg/config"
)

// compile-time: DiskScanner satisfies KeyScanner.
var _ KeyScanner = (*DiskScanner)(nil)

// Throwaway sample public keys (generated for tests; no real secrets).
const (
	pubED  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOnXKPflTu3pBUu9frCSu0vaVdCgwoHSe5LQcBTZGLFK alice@laptop\n"
	pubRSA = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCi+n7z0jPcq+uP53RiOWRYG37cJtKDlGviq7ISe7THLSvmyIbsk6rTyKg9rLqoMYuRAn8XWjScPZS2Bbk1D4Bm2se99gBn4SlB0YGJ9xDtfQxh4zBUD/jj2zMZXNCL6iT+Gt5fV2vuVxfg5x0UAlGvr04TGQWKjVsSrZGP7MRT7u0TG6la7jB/PqxcI7FI4LWvMiYAVuRbMC5t/o1BcfDh9zDhC/xR1dUkvsx9a3MsHDQGekDcqsYqW/fPh8L3OBfB14k5l1vxlN466ult0aXbQWyeuAtKOTALawaWsXMKjjmajejAa5Cf9yTJhDs6qy+mKXa97EmZn/auPWIbLGPH bob@server\n"
	pubEC  = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBL7i2CrN3cpAE+Pw6K8/409lK6SzoW4Jo1zuMZvHvWEg/5Ulbut85X+fO1Bx5qOI2cqi763ctKhf/Svtj66TGmw=\n"

	fpED = "SHA256:WJ2qO5SZailDsTNYXJiD9YSPvHrUfDO+AfcDDmVgcfw"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	// id_ed: full pair. id_rsa: pub only (private missing). id_ec: full pair, no comment.
	write(t, filepath.Join(dir, "id_ed.pub"), pubED)
	write(t, filepath.Join(dir, "id_ed"), "PRIVATE")
	write(t, filepath.Join(dir, "id_rsa.pub"), pubRSA)
	write(t, filepath.Join(dir, "id_ec.pub"), pubEC)
	write(t, filepath.Join(dir, "id_ec"), "PRIVATE")
	// noise that must be ignored
	write(t, filepath.Join(dir, "config"), "Host x\n")
	write(t, filepath.Join(dir, "known_hosts"), "github.com ssh-rsa AAAA\n")
	write(t, filepath.Join(dir, "garbage.pub"), "not a key\n")

	got, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byID := map[config.IdentityID]config.Identity{}
	for _, id := range got {
		byID[id.ID] = id
	}

	if len(byID) != 3 {
		t.Fatalf("want 3 identities, got %d: %v", len(byID), keysOf(byID))
	}

	ed := byID["id_ed"]
	if ed.Algorithm != config.AlgED25519 {
		t.Errorf("id_ed algo = %q, want ed25519", ed.Algorithm)
	}
	if ed.Comment != "alice@laptop" {
		t.Errorf("id_ed comment = %q, want alice@laptop", ed.Comment)
	}
	if ed.Fingerprint != fpED {
		t.Errorf("id_ed fingerprint = %q, want %q", ed.Fingerprint, fpED)
	}
	if !ed.ExistsOnDisk {
		t.Errorf("id_ed ExistsOnDisk = false, want true")
	}
	if ed.PublicKeyPath != filepath.Join(dir, "id_ed.pub") {
		t.Errorf("id_ed PublicKeyPath = %q", ed.PublicKeyPath)
	}

	if rsa := byID["id_rsa"]; rsa.Algorithm != config.AlgRSA || rsa.ExistsOnDisk {
		t.Errorf("id_rsa: algo=%q ExistsOnDisk=%v, want rsa/false", rsa.Algorithm, rsa.ExistsOnDisk)
	}

	if ec := byID["id_ec"]; ec.Algorithm != config.AlgECDSA || ec.Comment != "" {
		t.Errorf("id_ec: algo=%q comment=%q, want ecdsa/empty", ec.Algorithm, ec.Comment)
	}
}

func TestScanMissingDir(t *testing.T) {
	got, err := New(filepath.Join(t.TempDir(), "does-not-exist")).Scan()
	if err != nil {
		t.Fatalf("want nil error for missing dir, got %v", err)
	}
	if got != nil {
		t.Fatalf("want nil slice, got %v", got)
	}
}

func keysOf(m map[config.IdentityID]config.Identity) []config.IdentityID {
	var out []config.IdentityID
	for k := range m {
		out = append(out, k)
	}
	return out
}
