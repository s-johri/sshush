package service

import (
	"testing"

	"github.com/s-johri/sshush/pkg/agent"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
)

// recordScanner records Generate/Delete calls for routing assertions.
type recordScanner struct {
	ids       []config.Identity
	generated []keys.GenerateOpts
	deleted   []string
}

func (r *recordScanner) Scan() ([]config.Identity, error) { return r.ids, nil }
func (r *recordScanner) Generate(o keys.GenerateOpts) (config.Identity, error) {
	r.generated = append(r.generated, o)
	return config.Identity{ID: config.IdentityID(o.Name), Name: o.Name}, nil
}
func (r *recordScanner) Delete(p string) error { r.deleted = append(r.deleted, p); return nil }

// compile-time: App satisfies Service.
var _ Service = (*App)(nil)

// --- fakes ---

type fakeScanner struct {
	ids []config.Identity
	err error
}

func (f fakeScanner) Scan() ([]config.Identity, error) { return f.ids, f.err }
func (f fakeScanner) Generate(keys.GenerateOpts) (config.Identity, error) {
	return config.Identity{}, nil
}
func (f fakeScanner) Delete(string) error { return nil }

type fakeConfig struct {
	model        *config.SshConfigModel
	err          error
	edits        []config.HostID
	deletes      []config.HostID
	added        []config.HostID
	deletedHosts []config.HostID
	attached     []config.HostID
	detached     []config.HostID
	saved        int
}

func (f *fakeConfig) Load() (*config.SshConfigModel, error) { return f.model, f.err }
func (f *fakeConfig) SetHostField(h config.HostID, k, v string) error {
	f.edits = append(f.edits, h+"/"+config.HostID(k)+"="+config.HostID(v))
	return nil
}
func (f *fakeConfig) DeleteHostField(h config.HostID, k string) error {
	f.deletes = append(f.deletes, h+"/"+config.HostID(k))
	return nil
}
func (f *fakeConfig) AddHostIdentity(h config.HostID, path string) error {
	f.attached = append(f.attached, h+"/"+config.HostID(path))
	return nil
}
func (f *fakeConfig) RemoveHostIdentity(h config.HostID, id config.IdentityID) error {
	f.detached = append(f.detached, h+"/"+config.HostID(id))
	return nil
}
func (f *fakeConfig) AddHost(h config.Host) error { f.added = append(f.added, h.ID); return nil }
func (f *fakeConfig) DeleteHost(h config.HostID) error {
	f.deletedHosts = append(f.deletedHosts, h)
	return nil
}
func (f *fakeConfig) Save() error { f.saved++; return nil }

type fakeAgent struct {
	keys       []agent.AgentKey
	err        error
	added      []string
	removed    []string
	removedAll int
}

func (f *fakeAgent) List() ([]agent.AgentKey, error) { return f.keys, f.err }
func (f *fakeAgent) Add(p string) error              { f.added = append(f.added, p); return nil }
func (f *fakeAgent) Remove(p string) error           { f.removed = append(f.removed, p); return nil }
func (f *fakeAgent) RemoveAll() error                { f.removedAll++; return nil }

func TestRefreshMerge(t *testing.T) {
	cfg := &fakeConfig{model: &config.SshConfigModel{
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
	cfg := &fakeConfig{model: &config.SshConfigModel{}}
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

func TestEditHostSavesAndRefreshes(t *testing.T) {
	cfg := &fakeConfig{model: &config.SshConfigModel{
		Hosts: map[config.HostID]config.Host{"web": {ID: "web", Name: "web"}},
	}}
	app := New(fakeScanner{}, cfg, &fakeAgent{})

	if err := app.EditHost("web", "User", "deploy"); err != nil {
		t.Fatalf("EditHost: %v", err)
	}
	if len(cfg.edits) != 1 || cfg.edits[0] != "web/User=deploy" {
		t.Errorf("edit not forwarded: %v", cfg.edits)
	}
	if cfg.saved != 1 {
		t.Errorf("Save called %d times, want 1", cfg.saved)
	}
	if app.model == nil {
		t.Error("snapshot not refreshed after edit")
	}
}

func TestDeleteHostFieldSavesAndRefreshes(t *testing.T) {
	cfg := &fakeConfig{model: &config.SshConfigModel{
		Hosts: map[config.HostID]config.Host{"web": {ID: "web", Name: "web"}},
	}}
	app := New(fakeScanner{}, cfg, &fakeAgent{})

	if err := app.DeleteHostField("web", "ForwardAgent"); err != nil {
		t.Fatalf("DeleteHostField: %v", err)
	}
	if len(cfg.deletes) != 1 || cfg.deletes[0] != "web/ForwardAgent" {
		t.Errorf("delete not forwarded: %v", cfg.deletes)
	}
	if cfg.saved != 1 {
		t.Errorf("Save called %d times, want 1", cfg.saved)
	}
}

func TestAttachDetachKeyRouting(t *testing.T) {
	cfg := &fakeConfig{model: &config.SshConfigModel{
		Hosts: map[config.HostID]config.Host{"web": {ID: "web", Name: "web"}},
	}}
	scan := fakeScanner{ids: []config.Identity{
		{ID: "id_ed", Path: "/k/id_ed", ExistsOnDisk: true},
	}}
	app := New(scan, cfg, &fakeAgent{})
	if _, err := app.Refresh(); err != nil {
		t.Fatal(err)
	}

	if err := app.AttachKey("web", "id_ed"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.attached) != 1 || cfg.attached[0] != "web//k/id_ed" {
		t.Errorf("attach not routed with key path: %v", cfg.attached)
	}
	if err := app.DetachKey("web", "id_ed"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.detached) != 1 || cfg.detached[0] != "web/id_ed" {
		t.Errorf("detach not routed: %v", cfg.detached)
	}
	// Attaching an agent-only/unknown key fails (no disk path).
	if err := app.AttachKey("web", "ghost"); err == nil {
		t.Error("attaching unknown key should error")
	}
}

func TestHostAddDeleteRouting(t *testing.T) {
	cfg := &fakeConfig{model: &config.SshConfigModel{
		Hosts: map[config.HostID]config.Host{"web": {ID: "web", Name: "web"}},
	}}
	app := New(fakeScanner{}, cfg, &fakeAgent{})

	if err := app.AddHost(config.Host{ID: "db", Name: "db"}); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteHost("web"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.added) != 1 || cfg.added[0] != "db" {
		t.Errorf("add not routed: %v", cfg.added)
	}
	if len(cfg.deletedHosts) != 1 || cfg.deletedHosts[0] != "web" {
		t.Errorf("delete not routed: %v", cfg.deletedHosts)
	}
	if cfg.saved != 2 {
		t.Errorf("Save called %d times, want 2", cfg.saved)
	}
}

func TestKeyGenerateDeleteRouting(t *testing.T) {
	scan := &recordScanner{ids: []config.Identity{
		{ID: "k", Path: "/keys/k", ExistsOnDisk: true},
	}}
	cfg := &fakeConfig{model: &config.SshConfigModel{}}
	app := New(scan, cfg, &fakeAgent{})

	if _, err := app.GenerateKey(keys.GenerateOpts{Name: "newkey", Algorithm: config.AlgED25519}); err != nil {
		t.Fatal(err)
	}
	if len(scan.generated) != 1 || scan.generated[0].Name != "newkey" {
		t.Errorf("generate not routed: %v", scan.generated)
	}

	// Refresh populated the snapshot from scan.ids; "k" is deletable.
	if err := app.DeleteKey("k"); err != nil {
		t.Fatal(err)
	}
	if len(scan.deleted) != 1 || scan.deleted[0] != "/keys/k" {
		t.Errorf("delete not routed to path: %v", scan.deleted)
	}
	// Unknown / agent-only keys cannot be deleted.
	if err := app.DeleteKey("ghost"); err == nil {
		t.Error("deleting unknown key should error")
	}
}

func TestUnloadAllKeys(t *testing.T) {
	ag := &fakeAgent{}
	app := New(fakeScanner{}, &fakeConfig{model: &config.SshConfigModel{}}, ag)
	if err := app.UnloadAllKeys(); err != nil {
		t.Fatal(err)
	}
	if ag.removedAll != 1 {
		t.Errorf("RemoveAll called %d times, want 1", ag.removedAll)
	}
}

func TestDeleteKeyUnloadsFromAgentFirst(t *testing.T) {
	scan := &recordScanner{ids: []config.Identity{
		{ID: "k", Path: "/keys/k", Fingerprint: "FP", ExistsOnDisk: true},
	}}
	ag := &fakeAgent{keys: []agent.AgentKey{{Fingerprint: "FP"}}} // matches => loaded
	app := New(scan, &fakeConfig{model: &config.SshConfigModel{}}, ag)

	if _, err := app.Refresh(); err != nil {
		t.Fatal(err)
	}
	if !app.model.Identities["k"].LoadedInAgent {
		t.Fatal("precondition: key should be loaded in agent")
	}

	if err := app.DeleteKey("k"); err != nil {
		t.Fatal(err)
	}
	if len(ag.removed) != 1 || ag.removed[0] != "/keys/k" {
		t.Errorf("key not unloaded from agent before delete: %v", ag.removed)
	}
	if len(scan.deleted) != 1 || scan.deleted[0] != "/keys/k" {
		t.Errorf("key files not deleted: %v", scan.deleted)
	}
}

func TestRefreshNoAgentDegrades(t *testing.T) {
	cfg := &fakeConfig{model: &config.SshConfigModel{}}
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
