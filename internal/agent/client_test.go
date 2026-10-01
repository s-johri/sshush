package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// compile-time: Client satisfies AgentClient.
var _ AgentClient = (*Client)(nil)

// serveKeyring starts an in-process agent on a temp unix socket and returns its
// path. The agent serves the given keyring until the test ends.
func serveKeyring(t *testing.T, kr agent.Agent) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed at cleanup
			}
			go agent.ServeAgent(kr, conn)
		}
	}()
	return sock
}

func TestListMatchesFingerprint(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	wantFP := ssh.FingerprintSHA256(sshPub)

	kr := agent.NewKeyring()
	if err := kr.Add(agent.AddedKey{PrivateKey: priv, Comment: "test@host"}); err != nil {
		t.Fatal(err)
	}
	sock := serveKeyring(t, kr)

	got, err := New(sock).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 key, got %d", len(got))
	}
	if got[0].Fingerprint != wantFP {
		t.Errorf("fingerprint = %q, want %q", got[0].Fingerprint, wantFP)
	}
	if got[0].Comment != "test@host" {
		t.Errorf("comment = %q, want test@host", got[0].Comment)
	}
	if got[0].Format != ssh.KeyAlgoED25519 {
		t.Errorf("format = %q, want %q", got[0].Format, ssh.KeyAlgoED25519)
	}
}

func TestCommandBuilders(t *testing.T) {
	add := AddCommand("/k/id_ed25519")
	if got := add.Args[len(add.Args)-1]; got != "/k/id_ed25519" {
		t.Errorf("AddCommand path arg = %q", got)
	}
	rm := RemoveCommand("/k/id_ed25519")
	if len(rm.Args) < 3 || rm.Args[1] != "-d" {
		t.Errorf("RemoveCommand args = %v, want ssh-add -d <path>", rm.Args)
	}

	// c.Sock must be injected into the subprocess environment.
	c := New("/tmp/agent.sock")
	if env := c.addCmd("/k/x").Env; !envHas(env, "SSH_AUTH_SOCK=/tmp/agent.sock") {
		t.Errorf("addCmd env missing socket override: %v", env)
	}
}

func envHas(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

func TestListNoAgent(t *testing.T) {
	// Nonexistent socket path => unreachable => ErrNoAgent.
	_, err := New(filepath.Join(t.TempDir(), "missing.sock")).List()
	if !errors.Is(err, ErrNoAgent) {
		t.Fatalf("want ErrNoAgent, got %v", err)
	}
}
