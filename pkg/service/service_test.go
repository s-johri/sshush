package service

import (
	"testing"

	"github.com/s-johri/sshush/pkg/agent"
	"github.com/s-johri/sshush/pkg/config"
)

// compile-time: App satisfies Service.
var _ Service = (*App)(nil)

// --- fakes ---

type fakeScanner struct {
	ids []config.Identity
	err error
}

func (f fakeScanner) Scan() ([]config.Identity, error) { return f.ids, f.err }

type fakeConfig struct {
	model *config.SshConfigModel
	err   error
}

func (f fakeConfig) Load() (*config.SshConfigModel, error)             { return f.model, f.err }
func (f fakeConfig) SetHostField(config.HostID, string, string) error { return nil }
func (f fakeConfig) AddHost(config.Host) error                         { return nil }
func (f fakeConfig) DeleteHost(config.HostID) error                    { return nil }
func (f fakeConfig) Save() error                                       { return nil }

type fakeAgent struct {
	keys    []agent.AgentKey
	err     error
	added   []string
	removed []string
}

func (f *fakeAgent) List() ([]agent.AgentKey, error) { return f.keys, f.err }
func (f *fakeAgent) Add(p string) error              { f.added = append(f.added, p); return nil }
func (f *fakeAgent) Remove(p string) error           { f.removed = append(f.removed, p); return nil }

func TestRefreshMerge(t *testing.T) {
	cfg := fakeConfig{model: &config.SshConfigModel{
		Hosts:       map[config.HostID]config.Host{"web": {ID: "web", Name: "web"}},
		SourceFiles: []string{"/x/config"},
	}}
	scan := fakeScanner{ids: []config.Identity{
		{ID: "loaded", Fingerprint: "SHA256:AAA", ExistsOnDisk: true},
		{ID: "cold", Fingerprint: "SHA256:BBB", ExistsOnDisk: true},
	}}
	ag := &fakeAgent{keys: []agent.AgentKey{
		{Fingerprint: "SHA256:AAA", Comment: "me@box"},      // matches "loaded"
		{Fingerprint: "SHA256:ZZZ", Comment: "orphan@host"}, // agent-only
	}}

	model, err := New(scan, cfg, ag).Refresh()
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if got := model.Identities["loaded"]; !got.LoadedInAgent || got.AgentFingerprint != "SHA256:AAA" {
		t.Errorf("loaded: LoadedInAgent=%v AgentFingerprint=%q", got.LoadedInAgent, got.AgentFingerprint)
	}
	if got := model.Identities["cold"]; got.LoadedInAgent {
		t.Errorf("cold should be unloaded")
	}

	synth := model.Identities["agent:SHA256:ZZZ"]
	if !synth.LoadedInAgent || synth.ExistsOnDisk || synth.Name != "orphan@host" {
		t.Errorf("synthetic agent identity wrong: %+v", synth)
	}

	if _, ok := model.Hosts["web"]; !ok {
		t.Errorf("hosts not preserved through merge")
	}
}

func TestAgentMutationRouting(t *testing.T) {
	cfg := fakeConfig{model: &config.SshConfigModel{}}
	scan := fakeScanner{ids: []config.Identity{
		{ID: "disk", Path: "/k/disk", Fingerprint: "SHA256:AAA", ExistsOnDisk: true},
	}}
	ag := &fakeAgent{}
	app := New(scan, cfg, ag)

	// Mutating before Refresh must fail (no snapshot).
	if err := app.AddKeyToAgent("disk"); err == nil {
		t.Fatal("AddKeyToAgent before Refresh should error")
	}

	if _, err := app.Refresh(); err != nil {
		t.Fatal(err)
	}

	if err := app.AddKeyToAgent("disk"); err != nil {
		t.Fatalf("AddKeyToAgent: %v", err)
	}
	if len(ag.added) != 1 || ag.added[0] != "/k/disk" {
		t.Errorf("Add not routed to key path: %v", ag.added)
	}

	if err := app.RemoveKeyFromAgent("disk"); err != nil {
		t.Fatalf("RemoveKeyFromAgent: %v", err)
	}
	if len(ag.removed) != 1 || ag.removed[0] != "/k/disk" {
		t.Errorf("Remove not routed: %v", ag.removed)
	}

	// Agent-only synthetic identity has no disk path => cannot mutate.
	app.model.Identities["agent:x"] = config.Identity{ID: "agent:x", LoadedInAgent: true}
	if err := app.RemoveKeyFromAgent("agent:x"); err == nil {
		t.Error("removing agent-only key should error")
	}
	if err := app.AddKeyToAgent("ghost"); err == nil {
		t.Error("unknown identity should error")
	}
}

func TestRefreshNoAgentDegrades(t *testing.T) {
	cfg := fakeConfig{model: &config.SshConfigModel{}}
	scan := fakeScanner{ids: []config.Identity{{ID: "k", Fingerprint: "SHA256:AAA"}}}
	ag := &fakeAgent{err: agent.ErrNoAgent}

	model, err := New(scan, cfg, ag).Refresh()
	if err != nil {
		t.Fatalf("Refresh should degrade on ErrNoAgent, got %v", err)
	}
	if model.Identities["k"].LoadedInAgent {
		t.Errorf("no agent => key must be unloaded")
	}
	if len(model.Identities) != 1 {
		t.Errorf("no synthetic identities expected, got %d", len(model.Identities))
	}
}
