package sshconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/s-johri/sshush/pkg/config"
)

// compile-time: FileRepo satisfies ConfigRepo.
var _ ConfigRepo = (*FileRepo)(nil)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWithInclude(t *testing.T) {
	dir := t.TempDir()
	incPath := filepath.Join(dir, "extra.conf")
	mainPath := filepath.Join(dir, "config")

	writeFile(t, incPath, "Host db\n    HostName 10.0.0.5\n    User postgres\n    Port 2222\n")

	main := "# my ssh config\n" +
		"Host *\n" +
		"    ServerAliveInterval 60\n\n" +
		"Host web prod-web\n" +
		"    HostName 93.184.216.34\n" +
		"    User deploy\n" +
		"    IdentityFile ~/.ssh/id_ed25519\n" +
		"    ForwardAgent yes  # trust this box\n\n" +
		"Include " + incPath + "\n"
	writeFile(t, mainPath, main)

	r := New(mainPath)
	model, err := r.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(model.SourceFiles) != 2 {
		t.Fatalf("SourceFiles = %v, want main + include", model.SourceFiles)
	}

	web, ok := model.Hosts["web prod-web"]
	if !ok {
		t.Fatalf("missing host 'web prod-web'; hosts=%v", hostIDs(model))
	}
	if web.Hostname != "93.184.216.34" || web.User != "deploy" {
		t.Errorf("web: hostname=%q user=%q", web.Hostname, web.User)
	}
	if len(web.Identities) != 1 || web.Identities[0] != config.IdentityID("id_ed25519") {
		t.Errorf("web identities = %v, want [id_ed25519]", web.Identities)
	}
	if web.Options["ForwardAgent"] != "yes" {
		t.Errorf("web ForwardAgent = %q, want yes", web.Options["ForwardAgent"])
	}

	db, ok := model.Hosts["db"]
	if !ok {
		t.Fatalf("missing included host 'db'; hosts=%v", hostIDs(model))
	}
	if db.Port != 2222 || db.User != "postgres" {
		t.Errorf("db: port=%d user=%q", db.Port, db.User)
	}

	if _, ok := model.Hosts["*"]; ok {
		t.Errorf("wildcard host '*' should not surface as a model host")
	}
}

// TestRoundTrip is the safety-critical guard: re-emitting every parsed file
// must reproduce its original bytes exactly, so writes never corrupt comments,
// ordering, or unknown options.
func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	incPath := filepath.Join(dir, "extra.conf")
	mainPath := filepath.Join(dir, "config")

	inc := "Host db\n    HostName 10.0.0.5\n    User postgres\n    Port 2222\n"
	writeFile(t, incPath, inc)
	main := "# my ssh config\n" +
		"Host web prod-web\n" +
		"    HostName 93.184.216.34\n" +
		"    User deploy\n" +
		"    IdentityFile ~/.ssh/id_ed25519\n" +
		"    ForwardAgent yes  # trust this box\n\n" +
		"Include " + incPath + "\n"
	writeFile(t, mainPath, main)

	r := New(mainPath)
	if _, err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, lf := range r.files {
		if got := lf.cfg.String(); got != string(lf.raw) {
			t.Errorf("round-trip mismatch for %s:\n--- got ---\n%q\n--- want ---\n%q",
				lf.path, got, string(lf.raw))
		}
	}
}

func TestLoadMissingMain(t *testing.T) {
	model, err := New(filepath.Join(t.TempDir(), "nope")).Load()
	if err != nil {
		t.Fatalf("want nil error for missing main, got %v", err)
	}
	if len(model.Hosts) != 0 {
		t.Fatalf("want empty model, got %v", model.Hosts)
	}
}

func hostIDs(m *config.SshConfigModel) []config.HostID {
	var out []config.HostID
	for k := range m.Hosts {
		out = append(out, k)
	}
	return out
}
