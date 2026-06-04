// Package service orchestrates key scanning, config parsing, and agent state
// into a single SshConfigModel and mediates all mutations. It is the only
// package the TUI depends on; the TUI performs no IO of its own.
package service

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/s-johri/sshush/pkg/agent"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
	"github.com/s-johri/sshush/pkg/knownhosts"
	"github.com/s-johri/sshush/pkg/perms"
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
	UnloadAllKeys() error
	EditHost(h config.HostID, field, val string) error
	DeleteHostField(h config.HostID, field string) error
	AttachKey(h config.HostID, id config.IdentityID) error
	DetachKey(h config.HostID, id config.IdentityID) error
	AddHost(config.Host) error
	DeleteHost(config.HostID) error
	GenerateKey(keys.GenerateOpts) (config.Identity, error)
	DeleteKey(config.IdentityID) error
	AuditPermissions() ([]perms.Issue, error)
	FixPermissions([]perms.Issue) error
	KnownHosts() ([]knownhosts.Entry, error)
	RemoveKnownHost(line int) error
	CanRestore() bool
	BackupPaths() []string
	RestoreBackup() ([]string, error)
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

// Refresh parses config, scans disk keys, lists agent keys, and merges them
// into a single snapshot. Disk keys are matched to agent keys by fingerprint:
// a match sets LoadedInAgent and AgentFingerprint. Agent keys with no matching
// disk key are surfaced as synthetic identities (ExistsOnDisk=false) so the
// agent's true state is visible. A missing agent degrades gracefully — keys
// simply show as unloaded. The merged model is cached for mutation methods.
func (a *App) Refresh() (*config.SshConfigModel, error) {
	model, err := a.Config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if model.Identities == nil {
		model.Identities = map[config.IdentityID]config.Identity{}
	}

	ids, err := a.Keys.Scan()
	if err != nil {
		return nil, fmt.Errorf("scan keys: %w", err)
	}
	for _, id := range ids {
		model.Identities[id.ID] = id
	}

	// Agent state is best-effort: never let an absent agent fail a refresh.
	agentKeys, err := a.Agent.List()
	if err != nil && !errors.Is(err, agent.ErrNoAgent) {
		return nil, fmt.Errorf("list agent: %w", err)
	}
	mergeAgent(model, agentKeys)

	a.model = model
	return model, nil
}

// mergeAgent marks disk identities loaded when their fingerprint is in the
// agent, and adds synthetic identities for agent keys with no disk match.
func mergeAgent(model *config.SshConfigModel, agentKeys []agent.AgentKey) {
	// Index disk identities by fingerprint for matching.
	byFP := map[string]config.IdentityID{}
	for id, ident := range model.Identities {
		if ident.Fingerprint != "" {
			byFP[ident.Fingerprint] = id
		}
	}

	for _, ak := range agentKeys {
		if id, ok := byFP[ak.Fingerprint]; ok {
			ident := model.Identities[id]
			ident.LoadedInAgent = true
			ident.AgentFingerprint = ak.Fingerprint
			model.Identities[id] = ident
			continue
		}
		// Agent key not on disk: surface it so the view reflects reality.
		synthID := config.IdentityID("agent:" + ak.Fingerprint)
		model.Identities[synthID] = config.Identity{
			ID:               synthID,
			Name:             agentName(ak),
			Comment:          ak.Comment,
			Fingerprint:      ak.Fingerprint,
			ExistsOnDisk:     false,
			LoadedInAgent:    true,
			AgentFingerprint: ak.Fingerprint,
		}
	}
}

// agentName picks a human label for an agent-only key: its comment, else fp.
func agentName(ak agent.AgentKey) string {
	if ak.Comment != "" {
		return ak.Comment
	}
	return ak.Fingerprint
}

// AddKeyToAgent loads the identity's private key into the agent. The identity
// must exist on disk; agent-only and missing keys cannot be added.
func (a *App) AddKeyToAgent(id config.IdentityID) error {
	ident, err := a.diskIdentity(id)
	if err != nil {
		return err
	}
	return a.Agent.Add(ident.Path)
}

// RemoveKeyFromAgent drops the identity's key from the agent (ssh-add -d needs
// the key file, so the identity must exist on disk).
func (a *App) RemoveKeyFromAgent(id config.IdentityID) error {
	ident, err := a.diskIdentity(id)
	if err != nil {
		return err
	}
	return a.Agent.Remove(ident.Path)
}

// UnloadAllKeys drops every identity from the agent in one call.
func (a *App) UnloadAllKeys() error {
	return a.Agent.RemoveAll()
}

// AuditPermissions checks the SSH directory, config files, and private keys for
// over-permissive modes that SSH would reject. Requires a prior Refresh.
func (a *App) AuditPermissions() ([]perms.Issue, error) {
	if a.model == nil {
		return nil, errors.New("no snapshot loaded; call Refresh first")
	}
	configFiles := a.model.SourceFiles
	var keyPaths []string
	for _, id := range a.model.Identities {
		if id.ExistsOnDisk && id.Path != "" {
			keyPaths = append(keyPaths, id.Path)
		}
	}
	return perms.Audit(a.sshDirHint(), configFiles, keyPaths), nil
}

// sshDirHint guesses the SSH directory from the cached snapshot (config dir, or
// a key's dir), or "" to let callers fall back to ~/.ssh.
func (a *App) sshDirHint() string {
	if a.model == nil {
		return ""
	}
	if len(a.model.SourceFiles) > 0 {
		return filepath.Dir(a.model.SourceFiles[0])
	}
	for _, id := range a.model.Identities {
		if id.Path != "" {
			return filepath.Dir(id.Path)
		}
	}
	return ""
}

// KnownHosts parses the known_hosts file under the SSH directory.
func (a *App) KnownHosts() ([]knownhosts.Entry, error) {
	path, err := knownhosts.Path(a.sshDirHint())
	if err != nil {
		return nil, err
	}
	return knownhosts.Parse(path)
}

// RemoveKnownHost deletes a known_hosts line (backing up the file first).
func (a *App) RemoveKnownHost(line int) error {
	path, err := knownhosts.Path(a.sshDirHint())
	if err != nil {
		return err
	}
	return knownhosts.Remove(path, line)
}

// FixPermissions chmods each issue to its suggested mode, stopping at the first
// error.
func (a *App) FixPermissions(issues []perms.Issue) error {
	for _, i := range issues {
		if err := perms.Fix(i); err != nil {
			return err
		}
	}
	return nil
}

// diskIdentity returns a cached identity that has a usable on-disk key path.
func (a *App) diskIdentity(id config.IdentityID) (config.Identity, error) {
	if a.model == nil {
		return config.Identity{}, errors.New("no snapshot loaded; call Refresh first")
	}
	ident, ok := a.model.Identities[id]
	if !ok {
		return config.Identity{}, fmt.Errorf("unknown identity %q", id)
	}
	if !ident.ExistsOnDisk || ident.Path == "" {
		return config.Identity{}, fmt.Errorf("identity %q has no key file on disk", id)
	}
	return ident, nil
}

// CanRestore reports whether a backup exists to revert the config to.
func (a *App) CanRestore() bool { return len(a.Config.BackupPaths()) > 0 }

// BackupPaths lists the config files that have a ".bak" snapshot to restore.
func (a *App) BackupPaths() []string { return a.Config.BackupPaths() }

// RestoreBackup reverts the config file(s) to their ".bak" snapshots, then
// refreshes the cached snapshot so callers see the reverted state. Returns the
// restored file paths.
func (a *App) RestoreBackup() ([]string, error) {
	restored, err := a.Config.Restore()
	if err != nil {
		return restored, err
	}
	if _, err := a.Refresh(); err != nil {
		return restored, err
	}
	return restored, nil
}

// EditHost sets field=val on host h, persists the change (backing up the file),
// and refreshes the cached snapshot so callers see the result.
func (a *App) EditHost(h config.HostID, field, val string) error {
	if err := a.Config.SetHostField(h, field, val); err != nil {
		return err
	}
	if err := a.Config.Save(); err != nil {
		return err
	}
	_, err := a.Refresh()
	return err
}

// DeleteHostField removes a directive from host h, persists (with backup), and
// refreshes the cached snapshot.
func (a *App) DeleteHostField(h config.HostID, field string) error {
	if err := a.Config.DeleteHostField(h, field); err != nil {
		return err
	}
	if err := a.Config.Save(); err != nil {
		return err
	}
	_, err := a.Refresh()
	return err
}

// AttachKey associates identity id with host h by writing an IdentityFile
// directive pointing at the key's path, then persists and refreshes.
func (a *App) AttachKey(h config.HostID, id config.IdentityID) error {
	ident, err := a.diskIdentity(id)
	if err != nil {
		return err
	}
	if err := a.Config.AddHostIdentity(h, ident.Path); err != nil {
		return err
	}
	if err := a.Config.Save(); err != nil {
		return err
	}
	_, err = a.Refresh()
	return err
}

// DetachKey removes identity id's IdentityFile directive from host h.
func (a *App) DetachKey(h config.HostID, id config.IdentityID) error {
	if err := a.Config.RemoveHostIdentity(h, id); err != nil {
		return err
	}
	if err := a.Config.Save(); err != nil {
		return err
	}
	_, err := a.Refresh()
	return err
}

// AddHost appends a new host, persists (with backup), and refreshes.
func (a *App) AddHost(h config.Host) error {
	if err := a.Config.AddHost(h); err != nil {
		return err
	}
	if err := a.Config.Save(); err != nil {
		return err
	}
	_, err := a.Refresh()
	return err
}

// DeleteHost removes a host, persists (with backup), and refreshes.
func (a *App) DeleteHost(h config.HostID) error {
	if err := a.Config.DeleteHost(h); err != nil {
		return err
	}
	if err := a.Config.Save(); err != nil {
		return err
	}
	_, err := a.Refresh()
	return err
}

// GenerateKey creates a new key pair on disk and refreshes the snapshot.
func (a *App) GenerateKey(opts keys.GenerateOpts) (config.Identity, error) {
	id, err := a.Keys.Generate(opts)
	if err != nil {
		return config.Identity{}, err
	}
	if _, err := a.Refresh(); err != nil {
		return id, err
	}
	return id, nil
}

// DeleteKey removes an identity's key files from disk and refreshes. If the key
// is loaded in the agent it is unloaded first (ssh-add -d reads the public key
// file, which must still exist). Agent removal is best-effort so a flaky agent
// never blocks the file deletion. The identity must exist on disk.
func (a *App) DeleteKey(id config.IdentityID) error {
	ident, err := a.diskIdentity(id)
	if err != nil {
		return err
	}
	if ident.LoadedInAgent {
		_ = a.Agent.Remove(ident.Path) // best-effort, before files are gone
	}
	if err := a.Keys.Delete(ident.Path); err != nil {
		return err
	}
	_, err = a.Refresh()
	return err
}
