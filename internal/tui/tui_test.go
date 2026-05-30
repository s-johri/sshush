package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
)

type fakeService struct {
	model        *config.SshConfigModel
	err          error
	edits        []string
	deletes      []string
	addedHosts   []config.Host
	deletedHosts []config.HostID
	generated    []keys.GenerateOpts
	deletedKeys  []config.IdentityID
	unloadedAll  int
	attached     []string
	detached     []string
}

func (f *fakeService) Refresh() (*config.SshConfigModel, error)   { return f.model, f.err }
func (f *fakeService) AddKeyToAgent(config.IdentityID) error      { return nil }
func (f *fakeService) RemoveKeyFromAgent(config.IdentityID) error { return nil }
func (f *fakeService) UnloadAllKeys() error                       { f.unloadedAll++; return nil }
func (f *fakeService) EditHost(h config.HostID, field, val string) error {
	f.edits = append(f.edits, string(h)+"."+field+"="+val)
	return nil
}
func (f *fakeService) DeleteHostField(h config.HostID, field string) error {
	f.deletes = append(f.deletes, string(h)+"."+field)
	return nil
}
func (f *fakeService) AttachKey(h config.HostID, id config.IdentityID) error {
	f.attached = append(f.attached, string(h)+"/"+string(id))
	return nil
}
func (f *fakeService) DetachKey(h config.HostID, id config.IdentityID) error {
	f.detached = append(f.detached, string(h)+"/"+string(id))
	return nil
}
func (f *fakeService) AddHost(h config.Host) error {
	f.addedHosts = append(f.addedHosts, h)
	return nil
}
func (f *fakeService) DeleteHost(h config.HostID) error {
	f.deletedHosts = append(f.deletedHosts, h)
	return nil
}
func (f *fakeService) GenerateKey(o keys.GenerateOpts) (config.Identity, error) {
	f.generated = append(f.generated, o)
	return config.Identity{ID: config.IdentityID(o.Name), Name: o.Name}, nil
}
func (f *fakeService) DeleteKey(id config.IdentityID) error {
	f.deletedKeys = append(f.deletedKeys, id)
	return nil
}

func snapshot() *config.SshConfigModel {
	return &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"zeta":  {ID: "zeta", Name: "zeta", Algorithm: config.AlgRSA},
			"alpha": {ID: "alpha", Name: "alpha", Algorithm: config.AlgED25519, LoadedInAgent: true},
		},
		Hosts: map[config.HostID]config.Host{
			"web": {ID: "web", Name: "web", User: "deploy", Hostname: "example.com", Port: 22},
		},
		SourceFiles: []string{"/home/u/.ssh/config"},
	}
}

func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func feed(m Model, msg tea.Msg) Model {
	out, _ := m.Update(msg)
	return out.(Model)
}

func TestSnapshotSortedAndRendered(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})

	if len(m.ids) != 2 || m.ids[0].Name != "alpha" || m.ids[1].Name != "zeta" {
		t.Fatalf("ids not sorted: %+v", m.ids)
	}

	view := m.View()
	if !strings.Contains(view, "alpha") || !strings.Contains(view, "✓") {
		t.Errorf("keys view missing loaded key/badge:\n%s", view)
	}
}

func TestNavClampAndTab(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})

	m = feed(m, key("j")) // down -> 1
	if m.cursor[paneKeys] != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor[paneKeys])
	}
	m = feed(m, key("j")) // clamp at last row
	if m.cursor[paneKeys] != 1 {
		t.Fatalf("cursor over-advanced to %d", m.cursor[paneKeys])
	}

	m = feed(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.active != paneHosts {
		t.Fatalf("tab did not switch to hosts pane: %d", m.active)
	}
	if v := m.View(); !strings.Contains(v, "deploy@example.com:22") {
		t.Errorf("hosts view missing destination:\n%s", v)
	}
}

func TestToggleAgentOnlyKeyRejected(t *testing.T) {
	snap := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"orphan": {ID: "orphan", Name: "orphan", LoadedInAgent: true, ExistsOnDisk: false},
		},
	}
	m := New(&fakeService{model: snap})
	m = feed(m, refreshedMsg{model: snap})

	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd != nil {
		t.Error("agent-only key should not dispatch ssh-add")
	}
	if !strings.Contains(m.status, "agent-only") {
		t.Errorf("status = %q, want agent-only rejection", m.status)
	}
}

func TestAgentDoneTriggersRefresh(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	out, cmd := m.Update(agentDoneMsg{verb: "loaded"})
	m = out.(Model)
	if cmd == nil {
		t.Error("successful agent action should trigger a refresh")
	}
	if !strings.Contains(m.status, "loaded") {
		t.Errorf("status = %q", m.status)
	}
}

func TestEditFlowConfirmWrites(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // switch to Hosts pane

	m = feed(m, key("e")) // open editor
	if m.mode != modeEdit {
		t.Fatalf("expected modeEdit, got %d", m.mode)
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // HostName -> User (prefilled "deploy")
	m = feed(m, key("2"))                     // -> "deploy2"
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeConfirm {
		t.Fatalf("expected modeConfirm, got %d", m.mode)
	}

	out, cmd := m.Update(key("y"))
	m = out.(Model)
	if cmd == nil {
		t.Fatal("confirm should dispatch the edit")
	}
	cmd() // execute the write command
	if len(svc.edits) != 1 || svc.edits[0] != "web.User=deploy2" {
		t.Errorf("edit not written correctly: %v", svc.edits)
	}
}

func TestEditFlowCancelNoWrite(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab})
	m = feed(m, key("e"))
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // to confirm
	m = feed(m, key("n"))                       // cancel

	if m.mode != modeNormal {
		t.Errorf("cancel should return to normal mode, got %d", m.mode)
	}
	if len(svc.edits) != 0 {
		t.Errorf("cancel must not write: %v", svc.edits)
	}
}

func TestAddOptionFlow(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // Hosts pane

	m = feed(m, key("e"))                       // open edit overlay
	m = feed(m, tea.KeyMsg{Type: tea.KeyCtrlO}) // add option from inside edit
	if m.mode != modeNewKey {
		t.Fatalf("expected modeNewKey, got %d", m.mode)
	}
	// type "ForwardAgent"
	for _, r := range "ForwardAgent" {
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // name -> value entry
	if m.mode != modeEdit || m.newKey != "ForwardAgent" {
		t.Fatalf("after key entry: mode=%d newKey=%q", m.mode, m.newKey)
	}
	for _, r := range "yes" {
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // -> confirm

	out, cmd := m.Update(key("y"))
	m = out.(Model)
	if cmd == nil {
		t.Fatal("confirm should dispatch")
	}
	cmd()
	if len(svc.edits) != 1 || svc.edits[0] != "web.ForwardAgent=yes" {
		t.Errorf("add option wrong: %v", svc.edits)
	}
}

func TestDeleteDirectiveFlow(t *testing.T) {
	snap := &config.SshConfigModel{
		Hosts: map[config.HostID]config.Host{
			"web": {ID: "web", Name: "web", User: "deploy",
				Options: map[string]string{"ForwardAgent": "yes"}},
		},
	}
	svc := &fakeService{model: snap}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snap})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // Hosts pane

	m = feed(m, key("e"))                     // edit; present fields = [User, ForwardAgent]
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // User -> ForwardAgent
	if m.activeField() != "ForwardAgent" {
		t.Fatalf("expected ForwardAgent active, got %q", m.activeField())
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyCtrlD}) // request delete
	if m.mode != modeConfirmDelete {
		t.Fatalf("expected modeConfirmDelete, got %d", m.mode)
	}
	out, cmd := m.Update(key("y"))
	m = out.(Model)
	cmd()
	if len(svc.deletes) != 1 || svc.deletes[0] != "web.ForwardAgent" {
		t.Errorf("delete wrong: %v", svc.deletes)
	}
}

func TestHelpLinePerPane(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	if !strings.Contains(m.View(), "load/unload") {
		t.Error("keys pane help should mention load/unload")
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab})
	v := m.View()
	if strings.Contains(v, "load/unload") {
		t.Error("hosts pane help should NOT mention load/unload")
	}
	if !strings.Contains(v, "e edit") {
		t.Error("hosts pane help should mention edit")
	}
}

func TestNewHostFlow(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // Hosts pane

	m = feed(m, key("n"))
	if m.mode != modeNewHost {
		t.Fatalf("expected modeNewHost, got %d", m.mode)
	}
	// step 0: alias
	for _, r := range "db" {
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // -> HostName
	// step 1: HostName
	for _, r := range "10.0.0.1" {
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // -> User
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // skip User -> Port
	// step 3: Port
	for _, r := range "2200" {
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // basic fields done -> options loop
	if m.mode != modeNewHostOptKey {
		t.Fatalf("expected options loop, got mode %d", m.mode)
	}
	// blank option name finishes the wizard
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("blank option should dispatch AddHost")
	}
	cmd()
	if len(svc.addedHosts) != 1 {
		t.Fatalf("host not added: %v", svc.addedHosts)
	}
	h := svc.addedHosts[0]
	if h.Name != "db" || h.Hostname != "10.0.0.1" || h.Port != 2200 || h.User != "" {
		t.Errorf("wizard collected wrong host: %+v", h)
	}
}

func TestNewHostWithCustomOption(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // Hosts pane
	m = feed(m, key("n"))

	for _, r := range "web" { // alias
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // -> HostName
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // skip -> User
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // skip -> Port
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // skip -> options loop

	for _, r := range "ForwardAgent" {
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // name -> value
	if m.mode != modeNewHostOptVal {
		t.Fatalf("expected option value mode, got %d", m.mode)
	}
	for _, r := range "yes" {
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // value stored -> back to option name
	if m.mode != modeNewHostOptKey {
		t.Fatalf("expected to loop back for another option, got %d", m.mode)
	}
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // blank -> finish
	m = out.(Model)
	cmd()

	if len(svc.addedHosts) != 1 {
		t.Fatalf("host not added: %v", svc.addedHosts)
	}
	if got := svc.addedHosts[0].Options["ForwardAgent"]; got != "yes" {
		t.Errorf("custom option not collected: %v", svc.addedHosts[0].Options)
	}
}

func TestNewHostInvalidPort(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab})
	m = feed(m, key("n"))
	for _, r := range "db" {
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // -> HostName
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // -> User
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // -> Port
	for _, r := range "abc" {
		m = feed(m, key(string(r)))
	}
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd != nil || m.mode != modeNewHost {
		t.Error("invalid port should not dispatch; should stay on Port step")
	}
	if !strings.Contains(m.status, "port must be a number") {
		t.Errorf("status = %q", m.status)
	}
}

func TestDeleteHostFlow(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // Hosts pane

	m = feed(m, key("d"))
	if m.mode != modeConfirmDelHost {
		t.Fatalf("expected modeConfirmDelHost, got %d", m.mode)
	}
	out, cmd := m.Update(key("y"))
	m = out.(Model)
	cmd()
	if len(svc.deletedHosts) != 1 || svc.deletedHosts[0] != "web" {
		t.Errorf("host not deleted: %v", svc.deletedHosts)
	}
}

func diskKeySnap() *config.SshConfigModel {
	return &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"id_ed": {ID: "id_ed", Name: "id_ed", Path: "/k/id_ed", ExistsOnDisk: true},
		},
	}
}

func TestDeleteKeyFlowConfirm(t *testing.T) {
	svc := &fakeService{model: diskKeySnap()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: diskKeySnap()}) // Keys pane is default

	m = feed(m, key("d"))
	if m.mode != modeConfirmDelKey {
		t.Fatalf("expected modeConfirmDelKey, got %d", m.mode)
	}
	if !strings.Contains(m.View(), "IRREVERSIBLE") {
		t.Error("key delete confirm should warn it is irreversible")
	}
	out, cmd := m.Update(key("y"))
	m = out.(Model)
	cmd()
	if len(svc.deletedKeys) != 1 || svc.deletedKeys[0] != "id_ed" {
		t.Errorf("key not deleted: %v", svc.deletedKeys)
	}
}

func TestDeleteAgentOnlyKeyRejected(t *testing.T) {
	snap := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"orphan": {ID: "orphan", Name: "orphan", LoadedInAgent: true, ExistsOnDisk: false},
		},
	}
	svc := &fakeService{model: snap}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snap})

	out, cmd := m.Update(key("d"))
	m = out.(Model)
	if m.mode != modeNormal || cmd != nil {
		t.Error("agent-only key delete should be rejected, not confirmed")
	}
	if !strings.Contains(m.status, "agent-only") {
		t.Errorf("status = %q", m.status)
	}
}

func TestNewKeyGenDispatches(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()}) // Keys pane

	m = feed(m, key("n"))
	if m.mode != modeNewKeyGen {
		t.Fatalf("expected modeNewKeyGen, got %d", m.mode)
	}
	for _, r := range "id_new" {
		m = feed(m, key(string(r)))
	}
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd == nil {
		t.Error("key generation should dispatch an ExecProcess command")
	}
	if m.mode != modeNormal {
		t.Errorf("mode should reset after dispatch, got %d", m.mode)
	}
}

func TestUnloadAllDispatches(t *testing.T) {
	svc := &fakeService{model: diskKeySnap()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: diskKeySnap()}) // Keys pane

	_, cmd := m.unloadAll()
	if cmd == nil {
		t.Fatal("unloadAll should dispatch")
	}
	msg, ok := cmd().(agentDoneMsg)
	if !ok || msg.verb != "all keys unloaded" {
		t.Fatalf("unexpected msg: %#v", msg)
	}
	if svc.unloadedAll != 1 {
		t.Errorf("UnloadAllKeys called %d times, want 1", svc.unloadedAll)
	}
}

func TestStatusIsEphemeral(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})

	m = feed(m, agentDoneMsg{verb: "hello"})
	if m.status != "hello" {
		t.Fatalf("status=%q", m.status)
	}

	// A tick before the TTL elapses must keep the status.
	m = feed(m, tickMsg{})
	if m.status != "hello" {
		t.Error("status cleared before its TTL")
	}

	// Once the TTL has elapsed, a tick clears it.
	m.statusSetAt = time.Now().Add(-2 * statusTTL)
	m = feed(m, tickMsg{})
	if m.status != "" {
		t.Errorf("status not expired after TTL: %q", m.status)
	}
}

func keyPickerSnap() *config.SshConfigModel {
	return &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"id_a": {ID: "id_a", Name: "id_a", Path: "/k/id_a", ExistsOnDisk: true},
			"id_b": {ID: "id_b", Name: "id_b", Path: "/k/id_b", ExistsOnDisk: true},
		},
		Hosts: map[config.HostID]config.Host{
			"web": {ID: "web", Name: "web", Identities: []config.IdentityID{"id_a"}},
		},
	}
}

func TestKeyPickerAttachDetach(t *testing.T) {
	svc := &fakeService{model: keyPickerSnap()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: keyPickerSnap()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // Hosts pane

	m = feed(m, key("i"))
	if m.mode != modeKeyPicker {
		t.Fatalf("expected modeKeyPicker, got %d", m.mode)
	}
	// disk keys sorted: id_a (attached), id_b (not). Cursor 0 = id_a -> detach.
	if !strings.Contains(m.View(), "✓") {
		t.Error("picker should mark attached key")
	}
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	cmd()
	if len(svc.detached) != 1 || svc.detached[0] != "web/id_a" {
		t.Errorf("enter on attached key should detach: %v", svc.detached)
	}
	if m.mode != modeKeyPicker {
		t.Error("picker should stay open after a toggle")
	}

	// Move to id_b and attach.
	m = feed(m, key("j"))
	out, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	cmd()
	if len(svc.attached) != 1 || svc.attached[0] != "web/id_b" {
		t.Errorf("enter on unattached key should attach: %v", svc.attached)
	}
}

func TestWildcardHostShownAndWarned(t *testing.T) {
	snap := &config.SshConfigModel{
		Hosts: map[config.HostID]config.Host{
			"*": {ID: "*", Name: "*", IsPattern: true,
				Options: map[string]string{"ServerAliveInterval": "60"}},
		},
	}
	svc := &fakeService{model: snap}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snap})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // Hosts pane

	if !strings.Contains(m.View(), "pattern defaults") {
		t.Error("wildcard host should be listed with a pattern marker")
	}
	// Delete confirm should warn it is a wildcard.
	m = feed(m, key("d"))
	if !strings.Contains(m.View(), "wildcard block") {
		t.Errorf("delete confirm should warn about wildcard:\n%s", m.View())
	}
}

type fakeWatcher struct {
	ch   chan struct{}
	dirs [][]string
}

func (f *fakeWatcher) Events() <-chan struct{} { return f.ch }
func (f *fakeWatcher) Watch(dirs []string) error {
	f.dirs = append(f.dirs, dirs)
	return nil
}

func TestReloadRefreshesWhenIdle(t *testing.T) {
	fw := &fakeWatcher{ch: make(chan struct{}, 1)}
	m := New(&fakeService{model: snapshot()}).WithWatcher(fw)
	m = feed(m, refreshedMsg{model: snapshot()})

	// applySnapshot should have pointed the watcher at the source dir + ~/.ssh.
	if len(fw.dirs) == 0 {
		t.Fatal("watcher dirs never configured")
	}
	last := fw.dirs[len(fw.dirs)-1]
	if !contains(last, "/home/u/.ssh") {
		t.Errorf("watched dirs missing config dir: %v", last)
	}

	out, cmd := m.Update(reloadMsg{})
	m = out.(Model)
	if !m.loading || cmd == nil {
		t.Error("idle reload should trigger a refresh")
	}
	if !strings.Contains(m.status, "reloaded") {
		t.Errorf("status = %q", m.status)
	}
}

func TestReloadMutedAfterSelfWrite(t *testing.T) {
	fw := &fakeWatcher{ch: make(chan struct{}, 1)}
	m := New(&fakeService{model: snapshot()}).WithWatcher(fw)
	m = feed(m, refreshedMsg{model: snapshot()})

	// A completed self-write mutes reloads briefly.
	m = feed(m, editDoneMsg{verb: "host saved"})
	m.loading = false // pretend the follow-up refresh finished

	out, cmd := m.Update(reloadMsg{}) // event from our own write
	m = out.(Model)
	if m.loading {
		t.Error("self-induced reload should be ignored, not refresh")
	}
	if strings.Contains(m.status, "reloaded") {
		t.Errorf("should not show external-reload status: %q", m.status)
	}
	if cmd == nil {
		t.Error("listener must still be re-armed")
	}

	// Once the mute window passes, external changes reload normally again.
	m.muteReloadUntil = time.Now().Add(-time.Second)
	out, _ = m.Update(reloadMsg{})
	m = out.(Model)
	if !m.loading {
		t.Error("after mute window, reload should refresh")
	}
}

func TestReloadDeferredDuringOverlay(t *testing.T) {
	fw := &fakeWatcher{ch: make(chan struct{}, 1)}
	m := New(&fakeService{model: snapshot()}).WithWatcher(fw)
	m = feed(m, refreshedMsg{model: snapshot()})
	m.mode = modeEdit // pretend an overlay is open

	out, _ := m.Update(reloadMsg{})
	m = out.(Model)
	if !m.pendingReload {
		t.Error("reload during overlay should be deferred")
	}
	if m.loading {
		t.Error("must not refresh while an overlay is open")
	}

	// Returning to normal and ticking flushes the deferred reload.
	m.mode = modeNormal
	out, cmd := m.Update(tickMsg{})
	m = out.(Model)
	if m.pendingReload || !m.loading || cmd == nil {
		t.Error("tick should flush a pending reload once idle")
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

type fakeSettings struct {
	def     config.IdentityID
	auto    bool
	set     []config.IdentityID
	cleared int
}

func (f *fakeSettings) DefaultIdentity() config.IdentityID { return f.def }
func (f *fakeSettings) AutoLoad() bool                     { return f.auto }
func (f *fakeSettings) SetDefaultIdentity(id config.IdentityID) error {
	f.set = append(f.set, id)
	f.def = id
	f.auto = true
	return nil
}
func (f *fakeSettings) ClearDefault() error {
	f.cleared++
	f.def = ""
	f.auto = false
	return nil
}

func TestSetDefaultKey(t *testing.T) {
	fs := &fakeSettings{}
	m := New(&fakeService{model: diskKeySnap()}).WithSettings(fs)
	m = feed(m, refreshedMsg{model: diskKeySnap()}) // Keys pane, id_ed on disk

	m = feed(m, key("s"))
	if len(fs.set) != 1 || fs.set[0] != "id_ed" {
		t.Errorf("default not set: %v", fs.set)
	}
	if !strings.Contains(m.status, "default key set") {
		t.Errorf("status = %q", m.status)
	}
	if !strings.Contains(m.View(), "★ default") {
		t.Error("default key should be marked")
	}

	// Pressing s again on the same key unsets the default.
	m = feed(m, key("s"))
	if fs.cleared != 1 {
		t.Errorf("second press should clear default, cleared=%d", fs.cleared)
	}
	if !strings.Contains(m.status, "unset") {
		t.Errorf("status = %q", m.status)
	}
}

func TestAutoLoadDefaultOnStartup(t *testing.T) {
	fs := &fakeSettings{def: "id_ed", auto: true}
	m := New(&fakeService{model: diskKeySnap()}).WithSettings(fs)

	out, cmd := m.Update(refreshedMsg{model: diskKeySnap()})
	m = out.(Model)
	if cmd == nil {
		t.Error("auto-load should dispatch a load command for the default key")
	}
	if !m.autoLoaded {
		t.Error("autoLoaded flag should be set")
	}
	if !strings.Contains(m.status, "loading default key") {
		t.Errorf("status = %q", m.status)
	}

	// A second refresh must not auto-load again.
	out, cmd = m.Update(refreshedMsg{model: diskKeySnap()})
	m = out.(Model)
	if cmd != nil {
		t.Error("auto-load should run only once")
	}
}

func TestAutoLoadSkipsWhenAlreadyLoaded(t *testing.T) {
	snap := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"id_ed": {ID: "id_ed", Name: "id_ed", Path: "/k/id_ed", ExistsOnDisk: true, LoadedInAgent: true},
		},
	}
	fs := &fakeSettings{def: "id_ed", auto: true}
	m := New(&fakeService{model: snap}).WithSettings(fs)

	out, cmd := m.Update(refreshedMsg{model: snap})
	m = out.(Model)
	if cmd != nil {
		t.Error("should not reload a key that is already in the agent")
	}
	if !m.autoLoaded {
		t.Error("autoLoaded should still be marked to avoid retrying")
	}
}

func TestRefreshErrorShown(t *testing.T) {
	m := New(&fakeService{})
	m = feed(m, refreshedMsg{err: errFake{}})
	if m.err == nil {
		t.Fatal("error not recorded")
	}
	if !strings.Contains(m.View(), "boom") {
		t.Errorf("error not shown in view:\n%s", m.View())
	}
}

type errFake struct{}

func (errFake) Error() string { return "boom" }
