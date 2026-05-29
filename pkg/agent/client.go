// Package agent talks to the running ssh-agent. Reads (listing loaded keys and
// their fingerprints) use the native agent protocol over $SSH_AUTH_SOCK;
// writes (add/remove) shell out to ssh-add so the terminal can prompt for
// passphrases on encrypted keys.
package agent

import "errors"

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
}

// Client is the default AgentClient. Sock overrides $SSH_AUTH_SOCK when set.
type Client struct {
	Sock string
}

// New returns a Client. Empty sock uses $SSH_AUTH_SOCK from the environment.
func New(sock string) *Client {
	return &Client{Sock: sock}
}

// List implements AgentClient.
func (c *Client) List() ([]AgentKey, error) {
	return nil, ErrNotImplemented
}

// Add implements AgentClient.
func (c *Client) Add(path string) error {
	return ErrNotImplemented
}

// Remove implements AgentClient.
func (c *Client) Remove(path string) error {
	return ErrNotImplemented
}
