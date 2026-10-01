// Package agent talks to the running ssh-agent. Reads (listing loaded keys and
// their fingerprints) use the native agent protocol over $SSH_AUTH_SOCK;
// writes (add/remove) shell out to ssh-add so the terminal can prompt for
// passphrases on encrypted keys.
package agent

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// ErrNotImplemented is returned by stubbed methods during scaffolding.
var ErrNotImplemented = errors.New("not implemented")

// ErrNoAgent indicates $SSH_AUTH_SOCK is unset or unreachable. Callers should
// degrade gracefully rather than treat this as fatal.
var ErrNoAgent = errors.New("ssh-agent not available")

// AgentKey is one identity currently loaded in the agent.
type AgentKey struct {
	Fingerprint string // SHA256:... fingerprint, matches Identity.AgentFingerprint
	Comment     string
	Format      string // e.g. ssh-ed25519, ssh-rsa
}

// AgentClient reads and mutates the set of keys loaded in the agent.
type AgentClient interface {
	List() ([]AgentKey, error)
	Add(path string) error
	Remove(path string) error
	RemoveAll() error
}

// Client is the default AgentClient. Sock overrides $SSH_AUTH_SOCK when set.
type Client struct {
	Sock string
}

// New returns a Client. Empty sock uses $SSH_AUTH_SOCK from the environment.
func New(sock string) *Client {
	return &Client{Sock: sock}
}

// List implements AgentClient. It connects to the agent over the unix socket
// and returns the loaded identities with their SHA256 fingerprints. Returns
// ErrNoAgent (wrapped) when the socket is unset or unreachable so callers can
// degrade gracefully.
func (c *Client) List() ([]AgentKey, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	keys, err := agent.NewClient(conn).List()
	if err != nil {
		return nil, fmt.Errorf("agent list: %w", err)
	}

	out := make([]AgentKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, AgentKey{
			Fingerprint: ssh.FingerprintSHA256(k),
			Comment:     k.Comment,
			Format:      k.Format,
		})
	}
	return out, nil
}

// dial opens the agent socket: c.Sock if set, else $SSH_AUTH_SOCK.
func (c *Client) dial() (net.Conn, error) {
	sock := c.Sock
	if sock == "" {
		sock = os.Getenv("SSH_AUTH_SOCK")
	}
	if sock == "" {
		return nil, ErrNoAgent
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoAgent, err)
	}
	return conn, nil
}

// Add implements AgentClient by shelling out to ssh-add. It inherits the
// process's terminal so ssh-add can prompt for a passphrase on an encrypted key
// (without a tty, ssh-add falls back to ssh-askpass, which may not exist). Used
// from non-TUI contexts such as `sshush load-default`. Inside a TUI, use
// AddCommand with tea.ExecProcess so the program yields the terminal instead.
func (c *Client) Add(path string) error {
	cmd := c.addCmd(path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh-add: %w", err)
	}
	return nil
}

// Remove implements AgentClient (ssh-add -d <path>).
func (c *Client) Remove(path string) error {
	return run(c.removeCmd(path))
}

// RemoveAll drops every identity from the agent (ssh-add -D).
func (c *Client) RemoveAll() error {
	return run(c.cmd("-D"))
}

func (c *Client) addCmd(path string) *exec.Cmd    { return c.cmd(path) }
func (c *Client) removeCmd(path string) *exec.Cmd { return c.cmd("-d", path) }

// cmd builds an ssh-add invocation honoring c.Sock when set.
func (c *Client) cmd(args ...string) *exec.Cmd {
	cmd := exec.Command("ssh-add", args...)
	if c.Sock != "" {
		cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+c.Sock)
	}
	return cmd
}

// run executes an ssh-add command, surfacing stderr in the returned error.
func run(cmd *exec.Cmd) error {
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("ssh-add: %s: %w", msg, err)
		}
		return fmt.Errorf("ssh-add: %w", err)
	}
	return nil
}

// AddCommand builds an `ssh-add <path>` command using the ambient
// $SSH_AUTH_SOCK. Intended for tea.ExecProcess, which yields the terminal so
// ssh-add can prompt for a passphrase, then resumes the TUI.
func AddCommand(path string) *exec.Cmd { return exec.Command("ssh-add", path) }

// RemoveCommand builds an `ssh-add -d <path>` command for tea.ExecProcess.
func RemoveCommand(path string) *exec.Cmd { return exec.Command("ssh-add", "-d", path) }
