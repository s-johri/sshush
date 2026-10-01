package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/s-johri/sshush/internal/config"
)

func TestValidateArgs(t *testing.T) {
	ok := [][]string{
		{"restore"}, {"restore", "--yes"}, {"restore", "-y"},
		{"install-extras"}, {"install-extras", "--refresh"},
		{"completion", "bash"},
		{"load-default"}, {"shell-init"}, {"update"}, {"version"}, {"help"},
	}
	for _, args := range ok {
		if err := validateArgs(args); err != nil {
			t.Errorf("validateArgs(%q) = %v, want nil", args, err)
		}
	}
	bad := [][]string{
		{"restore", "--help"}, {"restore", "--yes", "x"},
		{"install-extras", "--refres"},
		{"completion", "bash", "x"},
		{"load-default", "x"}, {"shell-init", "x"}, {"update", "--force"},
		{"version", "x"}, {"help", "x"},
	}
	for _, args := range bad {
		if err := validateArgs(args); err == nil {
			t.Errorf("validateArgs(%q) = nil, want an error", args)
		}
	}
}

func backupStub() *stubService {
	return &stubService{backups: []config.Backup{{
		File:       "/home/u/.ssh/config",
		Path:       "/state/config.bak",
		ModTime:    time.Date(2026, 9, 1, 10, 30, 0, 0, time.Local),
		PreRestore: "/state/config.bak.pre-restore",
	}}}
}

// TestRestoreRefusesWithoutTTYOrYes: a script that runs restore with no
// terminal and no --yes must not write anything.
func TestRestoreRefusesWithoutTTYOrYes(t *testing.T) {
	svc := backupStub()
	var out bytes.Buffer
	err := runRestore(svc, restoreOpts{in: strings.NewReader("y\n"), out: &out})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("err = %v, want one that names --yes", err)
	}
	if svc.restored != 0 {
		t.Errorf("restored %d times, want 0", svc.restored)
	}
}

func TestRestoreAsksAndShowsBackupTime(t *testing.T) {
	for _, tc := range []struct {
		answer string
		want   int
	}{{"y\n", 1}, {"Y\n", 1}, {"n\n", 0}, {"\n", 0}, {"", 0}} {
		svc := backupStub()
		var out bytes.Buffer
		err := runRestore(svc, restoreOpts{tty: true, in: strings.NewReader(tc.answer), out: &out})
		if err != nil {
			t.Fatal(err)
		}
		if svc.restored != tc.want {
			t.Errorf("answer %q: restored %d times, want %d", tc.answer, svc.restored, tc.want)
		}
		got := out.String()
		for _, s := range []string{"/home/u/.ssh/config", "2026-09-01 10:30", "[y/N]"} {
			if !strings.Contains(got, s) {
				t.Errorf("answer %q: output does not contain %q:\n%s", tc.answer, s, got)
			}
		}
	}
}

func TestRestoreYesSkipsPromptAndNamesPreRestore(t *testing.T) {
	svc := backupStub()
	var out bytes.Buffer
	if err := runRestore(svc, restoreOpts{yes: true, in: strings.NewReader(""), out: &out}); err != nil {
		t.Fatal(err)
	}
	if svc.restored != 1 {
		t.Errorf("restored %d times, want 1", svc.restored)
	}
	got := out.String()
	if strings.Contains(got, "[y/N]") {
		t.Errorf("--yes still asked:\n%s", got)
	}
	if !strings.Contains(got, "/state/config.bak.pre-restore") {
		t.Errorf("output does not name the pre-restore copy:\n%s", got)
	}
}

func TestRestoreNoBackup(t *testing.T) {
	svc := &stubService{}
	var out bytes.Buffer
	if err := runRestore(svc, restoreOpts{yes: true, out: &out}); err != nil {
		t.Fatal(err)
	}
	if svc.restored != 0 || !strings.Contains(out.String(), "no backup") {
		t.Errorf("restored %d, output %q", svc.restored, out.String())
	}
}

// TestMainRejectsUnknownArgument runs main in a child process: `sshush
// restore --help` must exit 2 and must not change the config.
func TestMainRejectsUnknownArgument(t *testing.T) {
	if os.Getenv("SSHUSH_TEST_MAIN") == "1" {
		os.Args = append([]string{"sshush"}, strings.Fields(os.Getenv("SSHUSH_TEST_ARGS"))...)
		main()
		os.Exit(0)
	}
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(sshDir, "config")
	if err := os.WriteFile(cfg, []byte("Host web\n    User new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg+".bak", []byte("Host web\n    User old\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// -v is not a flag: it stays free for a later --verbose (T17).
	for _, args := range []string{"restore --help", "install-extras --refres", "version x", "-v"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMainRejectsUnknownArgument$")
		cmd.Env = append(os.Environ(), "SSHUSH_TEST_MAIN=1", "SSHUSH_TEST_ARGS="+args,
			"HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
			"XDG_STATE_HOME="+filepath.Join(home, ".state"), "XDG_DATA_HOME="+filepath.Join(home, ".data"))
		err := cmd.Run()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 2 {
			t.Errorf("sshush %s: err = %v, want exit 2", args, err)
		}
	}
	if got, _ := os.ReadFile(cfg); string(got) != "Host web\n    User new\n" {
		t.Errorf("config changed: %q", got)
	}
}
