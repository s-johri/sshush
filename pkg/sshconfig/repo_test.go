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

// TestSetFieldPreservesFormatting guards the unsafe rawValue trick: editing a
// value must change only that value, keeping indentation and trailing comment.
func TestSetFieldPreservesFormatting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host web\n    HostName 1.2.3.4\n    User old  # deploy user\n")

	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHostField("web", "User", "deploy2"); err != nil {
		t.Fatal(err)
	}

	want := "Host web\n    HostName 1.2.3.4\n    User deploy2  # deploy user\n"
	if got := r.files[0].cfg.String(); got != want {
		t.Errorf("edit output:\n got %q\nwant %q", got, want)
	}
}

func TestAppendFieldIndented(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host web\n    HostName 1.2.3.4\n")

	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHostField("web", "Port", "2222"); err != nil {
		t.Fatal(err)
	}

	want := "Host web\n    HostName 1.2.3.4\n    Port 2222\n"
	if got := r.files[0].cfg.String(); got != want {
		t.Errorf("append output:\n got %q\nwant %q", got, want)
	}
}

func TestSaveWritesBackupAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	orig := "Host web\n    User old\n"
	writeFile(t, path, orig)

	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHostField("web", "User", "new"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "Host web\n    User new\n" {
		t.Errorf("written file = %q", got)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup not written: %v", err)
	}
	if string(bak) != orig {
		t.Errorf("backup = %q, want original %q", bak, orig)
	}
}

func TestDeleteHostField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host web\n    HostName 1.2.3.4\n    ForwardAgent yes\n    User deploy\n")

	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteHostField("web", "ForwardAgent"); err != nil {
		t.Fatal(err)
	}

	want := "Host web\n    HostName 1.2.3.4\n    User deploy\n"
	if got := r.files[0].cfg.String(); got != want {
		t.Errorf("delete output:\n got %q\nwant %q", got, want)
	}
	// Deleting an absent directive is a no-op, not an error.
	if err := r.DeleteHostField("web", "Nope"); err != nil {
		t.Errorf("deleting absent directive should not error: %v", err)
	}
}

func TestSetHostFieldUnknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host web\n    User old\n")
	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHostField("ghost", "User", "x"); err == nil {
		t.Error("expected error for unknown host")
	}
}

func hostIDs(m *config.SshConfigModel) []config.HostID {
	var out []config.HostID
	for k := range m.Hosts {
		out = append(out, k)
	}
	return out
}
