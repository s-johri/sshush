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

	star, ok := model.Hosts["*"]
	if !ok {
		t.Fatalf("wildcard host '*' should surface; hosts=%v", hostIDs(model))
	}
	if !star.IsPattern {
		t.Errorf("'*' host should be flagged IsPattern")
	}
	if star.Options["ServerAliveInterval"] != "60" {
		t.Errorf("wildcard options not parsed: %v", star.Options)
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

// TestIncludeHonorsSshDir verifies a relative Include resolves against the
// configured SshDir, not a hardcoded ~/.ssh, and that a config-less SshDir
// resolves the default config path under it.
func TestIncludeHonorsSshDir(t *testing.T) {
	sshDir := t.TempDir()
	cfgDir := t.TempDir() // config lives somewhere else entirely
	writeFile(t, filepath.Join(sshDir, "extra.conf"), "Host db\n    User postgres\n")
	writeFile(t, filepath.Join(cfgDir, "config"), "Include extra.conf\nHost web\n    User deploy\n")

	r := New(filepath.Join(cfgDir, "config"))
	r.SshDir = sshDir
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := model.Hosts["db"]; !ok {
		t.Errorf("relative Include not resolved against SshDir; hosts=%v", hostIDs(model))
	}
	if _, ok := model.Hosts["web"]; !ok {
		t.Errorf("main host missing")
	}
}

func TestEmptyPathUsesSshDirConfig(t *testing.T) {
	sshDir := t.TempDir()
	writeFile(t, filepath.Join(sshDir, "config"), "Host x\n    User u\n")
	r := New("") // no explicit path
	r.SshDir = sshDir
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := model.Hosts["x"]; !ok {
		t.Errorf("empty Path should load <SshDir>/config; hosts=%v", hostIDs(model))
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

func TestAddHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host old\n    User x\n")

	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	h := config.Host{
		ID: "new", Name: "new",
		Hostname: "1.2.3.4", User: "deploy", Port: 2200,
	}
	if err := r.AddHost(h); err != nil {
		t.Fatal(err)
	}

	want := "Host old\n    User x\nHost new\n    HostName 1.2.3.4\n    User deploy\n    Port 2200\n\n"
	if got := r.files[0].cfg.String(); got != want {
		t.Errorf("add host output:\n got %q\nwant %q", got, want)
	}

	// Duplicate rejected.
	if err := r.AddHost(h); err == nil {
		t.Error("adding duplicate host should error")
	}
}

func TestAddHostToMissingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config") // does not exist yet
	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.AddHost(config.Host{ID: "web", Name: "web", User: "me"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "Host web\n    User me\n\n" {
		t.Errorf("created config = %q", got)
	}
}

func TestDeleteHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host a\n    User x\nHost b\n    User y\n")

	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteHost("a"); err != nil {
		t.Fatal(err)
	}
	want := "Host b\n    User y\n"
	if got := r.files[0].cfg.String(); got != want {
		t.Errorf("delete host output:\n got %q\nwant %q", got, want)
	}
	if err := r.DeleteHost("ghost"); err == nil {
		t.Error("deleting unknown host should error")
	}
}

func TestHostIdentityAddRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host web\n    HostName 1.2.3.4\n")

	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.AddHostIdentity("web", "~/.ssh/id_ed25519"); err != nil {
		t.Fatal(err)
	}
	want := "Host web\n    HostName 1.2.3.4\n    IdentityFile ~/.ssh/id_ed25519\n"
	if got := r.files[0].cfg.String(); got != want {
		t.Errorf("attach output:\n got %q\nwant %q", got, want)
	}
	// Adding the same path again is a no-op.
	_ = r.AddHostIdentity("web", "~/.ssh/id_ed25519")
	if got := r.files[0].cfg.String(); got != want {
		t.Errorf("duplicate attach changed output: %q", got)
	}
	// Detach by identity id (basename of the path).
	if err := r.RemoveHostIdentity("web", "id_ed25519"); err != nil {
		t.Fatal(err)
	}
	if got := r.files[0].cfg.String(); got != "Host web\n    HostName 1.2.3.4\n" {
		t.Errorf("detach output: %q", got)
	}
}

func TestEditWildcardHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host *\n    ServerAliveInterval 60\n")

	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	// findHost must match the wildcard block.
	if err := r.SetHostField("*", "ServerAliveInterval", "120"); err != nil {
		t.Fatal(err)
	}
	if got := r.files[0].cfg.String(); got != "Host *\n    ServerAliveInterval 120\n" {
		t.Errorf("wildcard edit output: %q", got)
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
