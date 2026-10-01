package perms

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuditFlagsAndFixes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil { // group/other access on the dir
		t.Fatal(err)
	}
	key := filepath.Join(dir, "id_ed25519")
	cfg := filepath.Join(dir, "config")
	good := filepath.Join(dir, "id_good")
	write(t, key, 0o644)  // private key readable by others
	write(t, cfg, 0o664)  // config writable by group
	write(t, good, 0o600) // already fine

	issues := Audit(dir, []string{cfg}, []string{key, good})

	byPath := map[string]Issue{}
	for _, i := range issues {
		byPath[i.Path] = i
	}
	if _, ok := byPath[dir]; !ok {
		t.Errorf("dir 0755 should be flagged")
	}
	if i, ok := byPath[key]; !ok || i.Want != 0o600 {
		t.Errorf("key issue = %+v", i)
	}
	if i, ok := byPath[cfg]; !ok || i.Want != 0o600 {
		t.Errorf("config issue = %+v", i)
	}
	if _, ok := byPath[good]; ok {
		t.Errorf("0600 key should not be flagged")
	}

	// Fixing clears the issues.
	for _, i := range issues {
		if err := Fix(i); err != nil {
			t.Fatal(err)
		}
	}
	if rest := Audit(dir, []string{cfg}, []string{key, good}); len(rest) != 0 {
		t.Errorf("after fix, still flagged: %+v", rest)
	}
}

func TestAuditSkipsMissing(t *testing.T) {
	if issues := Audit("/no/such/dir", []string{"/no/cfg"}, []string{"/no/key"}); len(issues) != 0 {
		t.Errorf("missing paths should not be flagged: %+v", issues)
	}
}

func TestConfigReadableButNotWritableOK(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o700)
	cfg := filepath.Join(dir, "config")
	write(t, cfg, 0o644) // readable by others but NOT writable — ssh tolerates
	if issues := Audit(dir, []string{cfg}, nil); len(issues) != 0 {
		t.Errorf("0644 config should be OK (not writable by others): %+v", issues)
	}
}

func write(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
