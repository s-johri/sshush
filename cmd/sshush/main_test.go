package main

import (
	"testing"

	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
	"github.com/s-johri/sshush/pkg/knownhosts"
	"github.com/s-johri/sshush/pkg/perms"
)

// stubService implements service.Service; Refresh returns a fixed model and
// AddKeyToAgent records which identities were loaded.
type stubService struct {
	model *config.SshConfigModel
	added []config.IdentityID
}

func (s *stubService) Refresh() (*config.SshConfigModel, error) { return s.model, nil }
func (s *stubService) AddKeyToAgent(id config.IdentityID) error {
	s.added = append(s.added, id)
	return nil
}
func (s *stubService) RemoveKeyFromAgent(config.IdentityID) error       { return nil }
func (s *stubService) UnloadAllKeys() error                             { return nil }
func (s *stubService) EditHost(config.HostID, string, string) error     { return nil }
func (s *stubService) DeleteHostField(config.HostID, string) error      { return nil }
func (s *stubService) AttachKey(config.HostID, config.IdentityID) error { return nil }
func (s *stubService) DetachKey(config.HostID, config.IdentityID) error { return nil }
func (s *stubService) AddHost(config.Host) error                        { return nil }
func (s *stubService) DeleteHost(config.HostID) error                   { return nil }
func (s *stubService) GenerateKey(keys.GenerateOpts) (config.Identity, error) {
	return config.Identity{}, nil
}
func (s *stubService) DeleteKey(config.IdentityID) error        { return nil }
func (s *stubService) AuditPermissions() ([]perms.Issue, error) { return nil, nil }
func (s *stubService) FixPermissions([]perms.Issue) error       { return nil }
func (s *stubService) KnownHosts() ([]knownhosts.Entry, error)  { return nil, nil }
func (s *stubService) RemoveKnownHost(int) error                { return nil }
func (s *stubService) CanRestore() bool                         { return false }
func (s *stubService) BackupPaths() []string                    { return nil }
func (s *stubService) RestoreBackup() ([]string, error)         { return nil, nil }

func modelWith(id config.Identity) *config.SshConfigModel {
	return &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{id.ID: id},
	}
}

func TestApplyDefaultsLoadsEligible(t *testing.T) {
	model := &config.SshConfigModel{Identities: map[config.IdentityID]config.Identity{
		"id_a": {ID: "id_a", ExistsOnDisk: true},
		"id_b": {ID: "id_b", ExistsOnDisk: true, LoadedInAgent: true}, // already loaded
		"id_c": {ID: "id_c", ExistsOnDisk: false},                     // missing on disk
	}}
	svc := &stubService{model: model}
	if err := applyDefaults([]config.IdentityID{"id_a", "id_b", "id_c"}, svc); err != nil {
		t.Fatal(err)
	}
	if len(svc.added) != 1 || svc.added[0] != "id_a" {
		t.Errorf("expected only id_a loaded, got %v", svc.added)
	}
}

func TestApplyDefaultsNoOps(t *testing.T) {
	loaded := modelWith(config.Identity{ID: "id_ed", ExistsOnDisk: true, LoadedInAgent: true})

	// Empty list: no-op (doesn't even refresh-load anything).
	svc := &stubService{model: loaded}
	if err := applyDefaults(nil, svc); err != nil {
		t.Fatal(err)
	}
	if len(svc.added) != 0 {
		t.Errorf("empty list should load nothing: %v", svc.added)
	}

	// All ineligible (already loaded): no-op.
	svc = &stubService{model: loaded}
	if err := applyDefaults([]config.IdentityID{"id_ed"}, svc); err != nil {
		t.Fatal(err)
	}
	if len(svc.added) != 0 {
		t.Errorf("already-loaded should not reload: %v", svc.added)
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		if err := completion(sh); err != nil {
			t.Errorf("completion(%q): %v", sh, err)
		}
	}
	if err := completion("tcsh"); err == nil {
		t.Error("unknown shell should error")
	}
}

// TestProgramOptsNoColor: any non-empty NO_COLOR must select a program option,
// and an empty one must not. no-color.org disables color for any value, so
// "yes" counts as much as "1".
func TestProgramOptsNoColor(t *testing.T) {
	for _, v := range []string{"1", "yes", "0", "false", "anything"} {
		if got := programOpts(v); len(got) != 1 {
			t.Errorf("NO_COLOR=%q: got %d options, want 1", v, len(got))
		}
	}
	if got := programOpts(""); len(got) != 0 {
		t.Errorf("NO_COLOR unset: got %d options, want 0", len(got))
	}
}
