package sshconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// The tests in this file guard T9: for a host alias in more than one block,
// OpenSSH uses the first block (in Include-expanded order). sshush must show
// and edit that same block.

func TestDuplicateAliasShowsAndEditsFirstBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host a\n    User first\n\nHost a\n    User second\n")

	r := New(path)
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if h := model.Hosts["a"]; h.User != "first" || h.Duplicates != 1 {
		t.Fatalf("host a = User %q, Duplicates %d; want first, 1", h.User, h.Duplicates)
	}
	if err := r.SetHostField("a", "User", "edited"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if want := "Host a\n    User edited\n\nHost a\n    User second\n"; string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	model, err = r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if u := model.Hosts["a"].User; u != "edited" {
		t.Errorf("User after edit = %q, want edited", u)
	}
}

// TestDuplicateAliasIncludeAtTop: an Include before the main file's blocks
// comes first in OpenSSH order, so its block wins.
func TestDuplicateAliasIncludeAtTop(t *testing.T) {
	dir := t.TempDir()
	inc := filepath.Join(dir, "inc.conf")
	writeFile(t, inc, "Host a\n    User included\n")
	main := filepath.Join(dir, "config")
	writeFile(t, main, "Include "+inc+"\n\nHost a\n    User main\n")

	r := New(main)
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if h := model.Hosts["a"]; h.User != "included" || h.Duplicates != 1 {
		t.Fatalf("host a = User %q, Duplicates %d; want included, 1", h.User, h.Duplicates)
	}
	if err := r.SetHostField("a", "User", "edited"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(inc); string(got) != "Host a\n    User edited\n" {
		t.Errorf("included file = %q, want the edit there", got)
	}
	if got, _ := os.ReadFile(main); string(got) != "Include "+inc+"\n\nHost a\n    User main\n" {
		t.Errorf("main file changed: %q", got)
	}
}

// TestDuplicateAliasIncludeAtBottom: an Include after the main file's block
// comes later, so the main block wins.
func TestDuplicateAliasIncludeAtBottom(t *testing.T) {
	dir := t.TempDir()
	inc := filepath.Join(dir, "inc.conf")
	writeFile(t, inc, "Host a\n    User included\n")
	main := filepath.Join(dir, "config")
	writeFile(t, main, "Host a\n    User main\n\nInclude "+inc+"\n")

	model, err := New(main).Load()
	if err != nil {
		t.Fatal(err)
	}
	if h := model.Hosts["a"]; h.User != "main" || h.Duplicates != 1 {
		t.Errorf("host a = User %q, Duplicates %d; want main, 1", h.User, h.Duplicates)
	}
}

// TestDeleteDuplicateRemovesShownBlock: delete removes the block that the
// UI shows. The next block then shows, with no duplicate mark.
func TestDeleteDuplicateRemovesShownBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host a\n    User first\n\nHost a\n    User second\n")

	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteHost("a"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if h := model.Hosts["a"]; h.User != "second" || h.Duplicates != 0 {
		t.Errorf("host a = User %q, Duplicates %d; want second, 0", h.User, h.Duplicates)
	}
}

// TestHostOrderWithEqualsAndIndent: "Host=a" and an indented Host line are
// Host lines too, so the order stays right.
func TestHostOrderWithEqualsAndIndent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "Host=b\n    User b1\nHost\tc\n    User c1\n  Host a\n    User first\nHost a\n    User second\n")
	model, err := New(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if h := model.Hosts["a"]; h.User != "first" {
		t.Errorf("host a User = %q, want first", h.User)
	}
	if lines, _ := scanDirectives([]byte("Host=b\nHost\tc\n  Host a\nHost a\n")); len(lines) != 4 {
		t.Errorf("Host lines = %v, want 4", lines)
	}
}
