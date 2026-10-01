package sshconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s-johri/sshush/internal/config"
)

// The tests in this file guard T12.

func TestSetHostFieldRejectsBadDirectives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "Host a\n    User u\n")
	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{
		{"ForwadAgent", "yes"}, {"Port", "abc"}, {"Port", "70000"},
		{"HostName", "a b"}, {"User", "x\nHost evil"},
	} {
		if err := r.SetHostField("a", kv[0], kv[1]); err == nil {
			t.Errorf("SetHostField(%q, %q) = nil, want an error", kv[0], kv[1])
		}
	}
	if err := r.AddHostIdentity("a", "/k/id\nHost evil"); err == nil {
		t.Error("AddHostIdentity accepted a path with a newline")
	}
}

func TestAddHostRejectsBadHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "Host a\n    User u\n")
	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	for _, h := range []config.Host{
		{ID: "a b", Name: "a b"},
		{ID: "n", Name: "n", Port: 70000},
		{ID: "n", Name: "n", User: "a b"},
		{ID: "n", Name: "n", Options: map[string][]string{"ForwadAgent": {"yes"}}},
	} {
		if err := r.AddHost(h); err == nil {
			t.Errorf("AddHost(%+v) = nil, want an error", h)
		}
	}
}

// fakeCheck rejects a file that contains "Compression maybe", as ssh would.
func fakeCheck(calls *int) func(string) error {
	return func(path string) error {
		*calls++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "Compression maybe") {
			return errors.New("Bad yes/no argument")
		}
		return nil
	}
}

func TestSaveRefusedWhenSshRejectsNewConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	orig := "Host a\n    User u\n"
	writeFile(t, path, orig)
	calls := 0
	r := New(path)
	r.Check = fakeCheck(&calls)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHostField("a", "Compression", "maybe"); err != nil {
		t.Fatal(err)
	}
	err := r.Save()
	if err == nil || !strings.Contains(err.Error(), "Bad yes/no") {
		t.Errorf("Save err = %v, want the ssh error", err)
	}
	if got, _ := os.ReadFile(path); string(got) != orig {
		t.Errorf("file changed:\n%s", got)
	}
	if calls == 0 {
		t.Error("Check was not called")
	}
}

// TestSaveAllowedWhenConfigAlreadyFailsSsh: when the current file already
// fails the check (for example an option that the local ssh is too old for),
// the check must not block every save.
func TestSaveAllowedWhenConfigAlreadyFailsSsh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "Host a\n    Compression maybe\n")
	calls := 0
	r := New(path)
	r.Check = fakeCheck(&calls)
	editAndSave(t, r, "a", "User", "v")
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "User v") {
		t.Errorf("edit not saved:\n%s", got)
	}
}

// TestSaveSkipsCheckWithMatchExec: ssh -G runs the command of a Match exec
// block, so the check must not run on such a file.
func TestSaveSkipsCheckWithMatchExec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "Host a\n    User u\n\nMatch exec \"touch /tmp/x\"\n    User m\n")
	calls := 0
	r := New(path)
	r.Check = fakeCheck(&calls)
	editAndSave(t, r, "a", "User", "v")
	if calls != 0 {
		t.Errorf("Check called %d times on a file with Match exec", calls)
	}
}

// TestEditExistingUnlistedOption: a directive that is already in the file is
// editable even when it is not in sshush's list (a newer OpenSSH option, or
// a name under IgnoreUnknown). Only a new directive name is checked.
func TestEditExistingUnlistedOption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "IgnoreUnknown UseKeychain\nHost a\n    UseKeychain yes\n")
	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHostField("a", "UseKeychain", "no"); err != nil {
		t.Errorf("edit of an existing directive: %v", err)
	}
	if err := r.SetHostField("a", "UseKeychian", "no"); err == nil {
		t.Error("a new unknown directive was accepted")
	}
}
