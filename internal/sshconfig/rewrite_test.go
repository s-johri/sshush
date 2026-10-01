package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file guard T11: a save writes the whole file from the
// parsed AST, which can change lines that the user did not edit. sshush must
// say so before the first write, and refuse a write that changes a value.

// reformatCases change only formatting: OpenSSH reads the same values.
var reformatCases = map[string]string{
	"tab indent":      "Host a\n\tUser u\n",
	"crlf":            "Host a\r\n    User u\r\n",
	"host equals":     "Host=a\n    User u\n",
	"indented host":   "  Host a\n    User u\n",
	"spaces in value": "Host a\n    User    u\n",
	"keyword case":    "HOST a\n    USER u\n",
	"kv equals":       "Host a\n    User=u\n",
	"pattern spaces":  "Host a  b\n    User u\n",
	"no last newline": "Host a\n    User u",
}

func TestRewriteCheckReformatOnly(t *testing.T) {
	for name, content := range reformatCases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			writeFile(t, path, content)
			r := New(path)
			model, err := r.Load()
			if err != nil {
				t.Fatal(err)
			}
			ids := hostIDs(model)
			if len(ids) != 1 {
				t.Fatalf("hosts = %v, want one", ids)
			}
			id := ids[0]
			c := r.RewriteCheck(id)
			if c.File != path || c.Reformat == "" || c.Unsafe != "" {
				t.Errorf("RewriteCheck = %+v, want a Reformat note and no Unsafe", c)
			}
			if c := r.RewriteCheck(""); c.File != path || c.Reformat == "" {
				t.Errorf("RewriteCheck(main) = %+v", c)
			}
			if err := r.SetHostField(id, "Port", "22"); err != nil {
				t.Fatal(err)
			}
			if err := r.Save(); err != nil {
				t.Fatalf("Save: %v", err)
			}
		})
	}
}

// TestRewriteCheckCleanFile: a file that round-trips needs no note.
func TestRewriteCheckCleanFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "# c\nHost a\n    User u # note\n    ProxyCommand ssh -W %h:%p   bastion\n")
	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if c := r.RewriteCheck("a"); c.Reformat != "" || c.Unsafe != "" {
		t.Errorf("RewriteCheck = %+v, want none", c)
	}
}

// TestRewriteCheckAfterOwnSave: once sshush wrote the file, the next save
// keeps every byte, so there is no note.
func TestRewriteCheckAfterOwnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "Host a\n\tUser u\n")
	r := New(path)
	editAndSave(t, r, "a", "User", "v")
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if c := r.RewriteCheck("a"); c.Reformat != "" || c.Unsafe != "" {
		t.Errorf("RewriteCheck after own save = %+v, want none", c)
	}
}

// TestRewriteRefusedWhenValueChanges: "User u#x" is written back as
// "User u #x", and OpenSSH then reads the user as "u". Save must refuse.
func TestRewriteRefusedWhenValueChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	orig := "Host a\n    User u#x\n\nHost b\n    User v\n"
	writeFile(t, path, orig)
	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	c := r.RewriteCheck("b")
	if c.Unsafe == "" || !strings.Contains(c.Unsafe, "u#x") {
		t.Errorf("RewriteCheck = %+v, want an Unsafe note that names u#x", c)
	}
	if err := r.SetHostField("b", "User", "w"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err == nil {
		t.Error("Save worked although it changes a value")
	}
	if got, _ := os.ReadFile(path); string(got) != orig {
		t.Errorf("file changed:\n%s", got)
	}
}

func TestDirectivesLikeOpenSSH(t *testing.T) {
	for line, want := range map[string]string{
		"User u#x":          "user|u#x",
		"User u #x":         "user|u",
		"User=u":            "user|u",
		"Host = a b":        "host|a|b",
		"  HOST\ta":         "host|a",
		`User "u v"`:        "user|u v",
		`User "u #v"`:       "user|u #v",
		"# comment":         "",
		"User u\r":          "user|u",
		"ProxyCommand a  b": "proxycommand|a|b",
	} {
		got := strings.Join(directiveTokens(line), "|")
		if got != want {
			t.Errorf("directiveTokens(%q) = %q, want %q", line, got, want)
		}
	}
}
