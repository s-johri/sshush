package main

import (
	"testing"

	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
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

func modelWith(id config.Identity) *config.SshConfigModel {
	return &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{id.ID: id},
	}
}

func TestApplyDefaultLoadsWhenEligible(t *testing.T) {
	svc := &stubService{model: modelWith(config.Identity{
		ID: "id_ed", ExistsOnDisk: true, LoadedInAgent: false,
	})}
	if err := applyDefault(true, "id_ed", svc); err != nil {
		t.Fatal(err)
	}
	if len(svc.added) != 1 || svc.added[0] != "id_ed" {
		t.Errorf("default not loaded: %v", svc.added)
	}
}

func TestApplyDefaultNoOps(t *testing.T) {
	loaded := modelWith(config.Identity{ID: "id_ed", ExistsOnDisk: true, LoadedInAgent: true})
	missing := &config.SshConfigModel{Identities: map[config.IdentityID]config.Identity{}}

	cases := []struct {
		name string
		auto bool
		id   config.IdentityID
		mdl  *config.SshConfigModel
	}{
		{"auto-load off", false, "id_ed", loaded},
		{"no default set", true, "", loaded},
		{"already loaded", true, "id_ed", loaded},
		{"key missing on disk", true, "id_ed", missing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &stubService{model: c.mdl}
			if err := applyDefault(c.auto, c.id, svc); err != nil {
				t.Fatal(err)
			}
			if len(svc.added) != 0 {
				t.Errorf("expected no load, got %v", svc.added)
			}
		})
	}
}
