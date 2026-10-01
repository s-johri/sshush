package sshconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s-johri/sshush/internal/config"
)

// The tests in this file guard T10: OpenSSH accepts many Match criteria that
// the ssh_config parser does not. A valid config must still load, the Match
// block must stay read-only, and a save must write it back byte for byte.

// unsupportedMatch are criteria that the parser rejects or misreads.
var unsupportedMatch = []string{
	"user bob",
	`exec "test -f /tmp/x"`,
	"originalhost x",
	"localuser me",
	"canonical",
	"final",
	"!host x",
	"localnetwork 10.0.0.0/8",
	"tagged t",
	"version OpenSSH_9*",
	"host x user bob",              // the parser reads "user bob" as host patterns
	"canonical host *.example.com", // two criteria
	"all user bob",
}

func matchFixture(crit string) string {
	return "Host a\n    User u\n\nMatch " + crit + "  # note\n    User m\n    ForwardAgent no\n\nHost b\n    User v\n"
}

func TestUnsupportedMatchLoadsAndRoundTrips(t *testing.T) {
	for _, crit := range unsupportedMatch {
		t.Run(crit, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			orig := matchFixture(crit)
			writeFile(t, path, orig)

			r := New(path)
			model, err := r.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if model.Hosts["a"].User != "u" || model.Hosts["b"].User != "v" {
				t.Errorf("hosts a/b = %+v / %+v", model.Hosts["a"], model.Hosts["b"])
			}
			id := config.HostID("Match " + crit)
			mh, ok := model.Hosts[id]
			if !ok {
				t.Fatalf("Match block not in model; hosts = %v", hostIDs(model))
			}
			if !mh.IsMatch || mh.MatchCriteria != "Match "+crit || mh.User != "m" {
				t.Errorf("Match block = %+v", mh)
			}
			if err := r.SetHostField(id, "User", "x"); !errors.Is(err, ErrMatchReadOnly) {
				t.Errorf("edit of Match block: err = %v, want ErrMatchReadOnly", err)
			}
			if err := r.DeleteHost(id); !errors.Is(err, ErrMatchReadOnly) {
				t.Errorf("delete of Match block: err = %v, want ErrMatchReadOnly", err)
			}

			// Edit another host in the same file: only that line changes.
			if err := r.SetHostField("b", "User", "w"); err != nil {
				t.Fatal(err)
			}
			if err := r.Save(); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			want := strings.Replace(orig, "User v", "User w", 1)
			if string(got) != want {
				t.Errorf("file after edit:\n%q\nwant\n%q", got, want)
			}
		})
	}
}

// TestUnsupportedMatchInIncludedFile: the same, in an included file.
func TestUnsupportedMatchInIncludedFile(t *testing.T) {
	dir := t.TempDir()
	inc := filepath.Join(dir, "inc.conf")
	orig := matchFixture("user bob")
	writeFile(t, inc, orig)
	main := filepath.Join(dir, "config")
	writeFile(t, main, "Include "+inc+"\n")

	r := New(main)
	model, err := r.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := model.Hosts["Match user bob"]; !ok {
		t.Fatalf("Match block not in model; hosts = %v", hostIDs(model))
	}
	if err := r.SetHostField("a", "User", "x"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(inc); string(got) != strings.Replace(orig, "User u", "User x", 1) {
		t.Errorf("included file after edit:\n%q", got)
	}
}

// TestNewKeyBeforeOpaqueMatchStaysInItsHost: a new directive for the host
// just before a Match block must be written in that host, not inside the
// Match block.
func TestNewKeyBeforeOpaqueMatchStaysInItsHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "Host a\n    User u\n\nMatch user bob\n    User m\n")
	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHostField("a", "Port", "2222"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "Match user bob\n    User m\n") {
		t.Errorf("Match block changed:\n%s", s)
	}
	if strings.Index(s, "Port 2222") > strings.Index(s, "Match user bob") {
		t.Errorf("new directive went into the Match block:\n%s", s)
	}
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if model.Hosts["a"].Port != 2222 {
		t.Errorf("host a Port = %d, want 2222", model.Hosts["a"].Port)
	}
}

// TestSupportedMatchUnchanged: Match all and Match host still go through the
// parser as before.
func TestSupportedMatchUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "Match host *.corp\n    User c\n\nMatch all\n    ForwardAgent no\n")
	model, err := New(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []config.HostID{"Match host *.corp", "Match all"} {
		if h, ok := model.Hosts[id]; !ok || !h.IsMatch {
			t.Errorf("%s not surfaced as a Match block: %v", id, hostIDs(model))
		}
	}
}
