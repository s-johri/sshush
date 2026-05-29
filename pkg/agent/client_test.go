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

func TestListNoAgent(t *testing.T) {
	// Nonexistent socket path => unreachable => ErrNoAgent.
	_, err := New(filepath.Join(t.TempDir(), "missing.sock")).List()
	if !errors.Is(err, ErrNoAgent) {
		t.Fatalf("want ErrNoAgent, got %v", err)
	}
}
