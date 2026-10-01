package sshconfig

import (
	"path/filepath"
	"slices"
	"testing"
)

// TestRepeatableDirectivesKeepEveryValue guards T15: ssh applies every
// LocalForward (and the other repeatable directives), so the model must keep
// all of them, in order.
func TestRepeatableDirectivesKeepEveryValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "Host a\n"+
		"    LocalForward 8080 localhost:80\n"+
		"    LocalForward 8443 localhost:443\n"+
		"    SendEnv LANG\n"+
		"    SendEnv LC_*\n")
	model, err := New(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	opts := model.Hosts["a"].Options
	if got := opts["LocalForward"]; !slices.Equal(got, []string{"8080 localhost:80", "8443 localhost:443"}) {
		t.Errorf("LocalForward = %q", got)
	}
	if got := opts["SendEnv"]; !slices.Equal(got, []string{"LANG", "LC_*"}) {
		t.Errorf("SendEnv = %q", got)
	}
}

// TestSingleDirectiveFirstValueWins: for a directive that ssh reads once, it
// uses the first value. The model must show that one, not the last.
func TestSingleDirectiveFirstValueWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "Host a\n"+
		"    HostName first.example\n    HostName second.example\n"+
		"    User first\n    User second\n"+
		"    Port 2201\n    Port 2202\n"+
		"    ProxyJump first\n    ProxyJump second\n")
	model, err := New(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	h := model.Hosts["a"]
	if h.Hostname != "first.example" || h.User != "first" || h.Port != 2201 {
		t.Errorf("host = %+v, want the first values", h)
	}
	if got := h.Options["ProxyJump"]; !slices.Equal(got, []string{"first"}) {
		t.Errorf("ProxyJump = %q, want [first]", got)
	}
}

// TestAddHostOptionAppends: adding a repeatable directive that the host
// already has adds a line; it does not change the existing one.
func TestAddHostOptionAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "Host a\n    LocalForward 8080 localhost:80\n")
	r := New(path)
	if _, err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.AddHostOption("a", "LocalForward", "8443 localhost:443"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	model, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := model.Hosts["a"].Options["LocalForward"]; !slices.Equal(got, []string{"8080 localhost:80", "8443 localhost:443"}) {
		t.Errorf("LocalForward = %q", got)
	}
	if err := r.AddHostOption("a", "ForwadAgent", "yes"); err == nil {
		t.Error("AddHostOption accepted an unknown option")
	}
}
