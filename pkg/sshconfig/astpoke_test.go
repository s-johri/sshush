package sshconfig

import (
	"strings"
	"testing"

	sshcfg "github.com/kevinburke/ssh_config"
)

// TestAstPokeBindings is the tripwire for the reflect/unsafe accessors in
// astpoke.go: it parses a fixture and asserts every poked unexported field
// still exists and behaves. A ssh_config dependency bump that changes the
// private AST shape fails here (often by panic) instead of corrupting writes.
func TestAstPokeBindings(t *testing.T) {
	src := "Port 22\n" + // before any Host: lands in the implicit global block
		"Host web\n" +
		"    HostName 1.2.3.4\n" +
		"\n" +
		"Match Host *.corp\n" +
		"    User corpuser\n"
	cfg, err := sshcfg.DecodeBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hosts) < 3 {
		t.Fatalf("fixture should parse to implicit + web + match, got %d hosts", len(cfg.Hosts))
	}
	implicit, web, match := cfg.Hosts[0], cfg.Hosts[1], cfg.Hosts[2]

	// implicit / isMatch flags
	if !isImplicitHost(implicit) || isImplicitHost(web) || isImplicitHost(match) {
		t.Error("isImplicitHost binding wrong")
	}
	if !isMatchHost(match) || isMatchHost(web) || isMatchHost(implicit) {
		t.Error("isMatchHost binding wrong")
	}

	// matchKeyword (original case preserved)
	if kw := matchKeywordOf(match); kw != "Host" {
		t.Errorf("matchKeywordOf = %q, want Host", kw)
	}

	// blockIndent reads the parsed leading space of the first directive.
	if got := blockIndent(web); got != 4 {
		t.Errorf("blockIndent = %d, want 4", got)
	}
	if got := blockIndent(&sshcfg.Host{}); got != 4 {
		t.Errorf("blockIndent default = %d, want 4", got)
	}

	// setKVValue must clear rawValue so String() emits the new value.
	kv, ok := web.Nodes[0].(*sshcfg.KV)
	if !ok {
		t.Fatal("fixture: first node of web should be a KV")
	}
	setKVValue(kv, "9.9.9.9")
	if out := cfg.String(); !strings.Contains(out, "HostName 9.9.9.9") {
		t.Errorf("setKVValue not reflected in output:\n%s", out)
	}

	// setKVIndent must control a fresh KV's emitted indentation.
	fresh := &sshcfg.KV{Key: "User", Value: "deploy"}
	setKVIndent(fresh, 4)
	web.Nodes = append(web.Nodes, fresh)
	if out := cfg.String(); !strings.Contains(out, "\n    User deploy") {
		t.Errorf("setKVIndent not reflected in output:\n%s", out)
	}
}
