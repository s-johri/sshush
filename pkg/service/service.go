// Package service orchestrates key scanning, config parsing, and agent state
// into a single SshConfigModel and mediates all mutations. It is the only
// package the TUI depends on; the TUI performs no IO of its own.
package service

import (
	"errors"

	"github.com/s-johri/sshush/pkg/agent"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
	"github.com/s-johri/sshush/pkg/sshconfig"
)

// ErrNotImplemented is returned by stubbed methods during scaffolding.
var ErrNotImplemented = errors.New("not implemented")

// Service is the façade the TUI talks to. Implementations merge disk, config,
// and agent state, and re-refresh after every mutation to keep state honest.
type Service interface {
	Refresh() (*config.SshConfigModel, error)
	AddKeyToAgent(config.IdentityID) error
	RemoveKeyFromAgent(config.IdentityID) error
	EditHost(h config.HostID, field, val string) error
	AddHost(config.Host) error
	DeleteHost(config.HostID) error
}

// App is the default Service, wiring the three repositories together.
type App struct {
	Keys   keys.KeyScanner
	Config sshconfig.ConfigRepo
	Agent  agent.AgentClient

	model *config.SshConfigModel // last merged snapshot
}

// New wires an App from its collaborators.
func New(k keys.KeyScanner, c sshconfig.ConfigRepo, a agent.AgentClient) *App {
	return &App{Keys: k, Config: c, Agent: a}
}

// Refresh scans disk + parses config + lists agent keys, merges by
// fingerprint, and caches the snapshot.
func (a *App) Refresh() (*config.SshConfigModel, error) {
	return nil, ErrNotImplemented
}

// AddKeyToAgent implements Service.
func (a *App) AddKeyToAgent(id config.IdentityID) error { return ErrNotImplemented }

// RemoveKeyFromAgent implements Service.
func (a *App) RemoveKeyFromAgent(id config.IdentityID) error { return ErrNotImplemented }

// EditHost implements Service.
func (a *App) EditHost(h config.HostID, field, val string) error { return ErrNotImplemented }

// AddHost implements Service.
func (a *App) AddHost(h config.Host) error { return ErrNotImplemented }

// DeleteHost implements Service.
func (a *App) DeleteHost(h config.HostID) error { return ErrNotImplemented }
