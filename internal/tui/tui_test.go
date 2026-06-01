package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/s-johri/sshush/pkg/clip"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
	"github.com/s-johri/sshush/pkg/knownhosts"
	"github.com/s-johri/sshush/pkg/perms"
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
	permIssues   []perms.Issue
	permErr      error
	fixedPerms   int
	khEntries    []knownhosts.Entry
	khErr        error
	khRemoved    []int
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
func (f *fakeService) AuditPermissions() ([]perms.Issue, error) { return f.permIssues, f.permErr }
func (f *fakeService) FixPermissions(is []perms.Issue) error {
	f.fixedPerms += len(is)
	return nil
}
func (f *fakeService) KnownHosts() ([]knownhosts.Entry, error) { return f.khEntries, f.khErr }
func (f *fakeService) RemoveKnownHost(line int) error {
	f.khRemoved = append(f.khRemoved, line)
	// Simulate removal so a re-fetch reflects it.
	var kept []knownhosts.Entry
	for _, e := range f.khEntries {
		if e.Line != line {
			kept = append(kept, e)
		}
	}
	f.khEntries = kept
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
	if !strings.Contains(view, "alpha") || !strings.Contains(view, "●") {
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

func TestNewKeyGenEd25519SkipsBits(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()}) // Keys pane

	m = feed(m, key("n"))
	if m.mode != modeNewKeyAlgo {
		t.Fatalf("expected modeNewKeyAlgo, got %d", m.mode)
	}
	// ed25519 is first; selecting it skips the bits step and prefills the name.
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeNewKeyGen {
		t.Fatalf("ed25519 should go straight to filename, got mode %d", m.mode)
	}
	if m.keyAlgo != config.AlgED25519 || m.keyBits != 0 {
		t.Errorf("algo=%q bits=%d", m.keyAlgo, m.keyBits)
	}
	if m.input.Value() != "id_ed25519" {
		t.Errorf("filename default = %q", m.input.Value())
	}
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd == nil || m.mode != modeNormal {
		t.Error("should dispatch keygen and reset mode")
	}
}

func TestNewKeyGenRsaPicksBits(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})

	m = feed(m, key("n"))
	m = feed(m, key("j"))                       // ed25519 -> rsa
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // select rsa -> bits step
	if m.mode != modeNewKeyBits || m.keyAlgo != config.AlgRSA {
		t.Fatalf("expected rsa bits step, mode=%d algo=%q", m.mode, m.keyAlgo)
	}
	m = feed(m, key("j"))                       // 3072 -> 4096
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // select 4096 -> filename
	if m.mode != modeNewKeyGen || m.keyBits != 4096 {
		t.Fatalf("bits not selected: mode=%d bits=%d", m.mode, m.keyBits)
	}
	if !strings.Contains(m.View(), "rsa, 4096 bits") {
		t.Errorf("filename step should summarize algo/bits:\n%s", m.View())
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
	if !strings.Contains(m.View(), "●") {
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
	defaults    []config.IdentityID
	toggles     []config.IdentityID
	motionOn    bool
	motionLevel string
	themeName   string
}

func (f *fakeSettings) MotionEnabled() bool { return f.motionOn }
func (f *fakeSettings) MotionIntensity() string {
	if f.motionLevel == "" {
		return "normal"
	}
	return f.motionLevel
}
func (f *fakeSettings) SetMotion(on bool, intensity string) error {
	f.motionOn = on
	if intensity != "" {
		f.motionLevel = intensity
	}
	return nil
}
func (f *fakeSettings) ThemeName() string       { return f.themeName }
func (f *fakeSettings) SetTheme(n string) error { f.themeName = n; return nil }

func (f *fakeSettings) DefaultIdentities() []config.IdentityID { return f.defaults }
func (f *fakeSettings) AutoLoad() bool                         { return len(f.defaults) > 0 }
func (f *fakeSettings) IsDefault(id config.IdentityID) bool {
	for _, d := range f.defaults {
		if d == id {
			return true
		}
	}
	return false
}
func (f *fakeSettings) ToggleDefault(id config.IdentityID) (bool, error) {
	f.toggles = append(f.toggles, id)
	for i, d := range f.defaults {
		if d == id {
			f.defaults = append(f.defaults[:i], f.defaults[i+1:]...)
			return false, nil
		}
	}
	f.defaults = append(f.defaults, id)
	return true, nil
}

func TestToggleDefaultKey(t *testing.T) {
	fs := &fakeSettings{}
	m := New(&fakeService{model: diskKeySnap()}).WithSettings(fs)
	m = feed(m, refreshedMsg{model: diskKeySnap()}) // Keys pane, id_ed on disk

	m = feed(m, key("s"))
	if !fs.IsDefault("id_ed") || !strings.Contains(m.status, "added default") {
		t.Errorf("first press should add default; status=%q defaults=%v", m.status, fs.defaults)
	}
	if !strings.Contains(m.View(), "★ default") {
		t.Error("default key should be marked")
	}

	m = feed(m, key("s"))
	if fs.IsDefault("id_ed") || !strings.Contains(m.status, "removed default") {
		t.Errorf("second press should remove; status=%q defaults=%v", m.status, fs.defaults)
	}
}

func TestAutoLoadMultipleDefaults(t *testing.T) {
	snap := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"id_a": {ID: "id_a", Name: "id_a", Path: "/k/id_a", ExistsOnDisk: true},
			"id_b": {ID: "id_b", Name: "id_b", Path: "/k/id_b", ExistsOnDisk: true, LoadedInAgent: true},
			"id_c": {ID: "id_c", Name: "id_c", Path: "/k/id_c", ExistsOnDisk: true},
		},
	}
	fs := &fakeSettings{defaults: []config.IdentityID{"id_a", "id_b", "id_c"}}
	m := New(&fakeService{model: snap}).WithSettings(fs)

	out, cmd := m.Update(refreshedMsg{model: snap})
	m = out.(Model)
	// id_b already loaded → only id_a and id_c get loaded.
	if cmd == nil {
		t.Error("auto-load should dispatch for the unloaded defaults")
	}
	if !m.autoLoaded {
		t.Error("autoLoaded should be set")
	}
	if !strings.Contains(m.status, "loading default keys") {
		t.Errorf("status = %q", m.status)
	}

	out, cmd = m.Update(refreshedMsg{model: snap})
	m = out.(Model)
	if cmd != nil {
		t.Error("auto-load should run only once")
	}
}

func TestAutoLoadSkipsWhenAllLoaded(t *testing.T) {
	snap := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"id_ed": {ID: "id_ed", Name: "id_ed", Path: "/k/id_ed", ExistsOnDisk: true, LoadedInAgent: true},
		},
	}
	fs := &fakeSettings{defaults: []config.IdentityID{"id_ed"}}
	m := New(&fakeService{model: snap}).WithSettings(fs)

	out, cmd := m.Update(refreshedMsg{model: snap})
	m = out.(Model)
	if cmd != nil {
		t.Error("should not reload an already-loaded default")
	}
	if !m.autoLoaded {
		t.Error("autoLoaded should still be marked")
	}
}

func TestKeysShowAssociatedHosts(t *testing.T) {
	snap := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"id_a": {ID: "id_a", Name: "id_a", ExistsOnDisk: true},
			"id_b": {ID: "id_b", Name: "id_b", ExistsOnDisk: true},
		},
		Hosts: map[config.HostID]config.Host{
			"prod": {ID: "prod", Name: "prod", Identities: []config.IdentityID{"id_a"}},
			"web":  {ID: "web", Name: "web", Identities: []config.IdentityID{"id_a"}},
		},
	}
	m := New(&fakeService{model: snap})
	m = feed(m, refreshedMsg{model: snap})

	view := m.View()
	// id_a is used by prod and web; both should appear on the keys pane.
	if !strings.Contains(view, "prod") || !strings.Contains(view, "web") {
		t.Errorf("keys pane should list hosts using each key:\n%s", view)
	}
	// reverse map sanity
	used := m.hostsByKey()
	if len(used["id_a"]) != 2 || len(used["id_b"]) != 0 {
		t.Errorf("hostsByKey wrong: %v", used)
	}
}

func manyKeysSnap(n int) *config.SshConfigModel {
	ids := map[config.IdentityID]config.Identity{}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("id_%02d", i)
		ids[config.IdentityID(name)] = config.Identity{ID: config.IdentityID(name), Name: name, ExistsOnDisk: true}
	}
	return &config.SshConfigModel{Identities: ids}
}

func TestScrollKeepsCursorVisible(t *testing.T) {
	snap := manyKeysSnap(40)
	m := New(&fakeService{model: snap})
	m = feed(m, tea.WindowSizeMsg{Width: 80, Height: 18})
	m = feed(m, refreshedMsg{model: snap})

	cap := m.listCapacity()
	if cap <= 0 || cap >= 40 {
		t.Fatalf("capacity = %d, want a bounded window", cap)
	}
	// Move past the first window; scroll should follow the cursor.
	for i := 0; i <= cap; i++ {
		m = feed(m, key("j"))
	}
	if m.cursor[paneKeys] != cap+1 {
		t.Fatalf("cursor = %d, want %d", m.cursor[paneKeys], cap+1)
	}
	if m.scroll[paneKeys] == 0 {
		t.Error("scroll should advance once cursor passes the first window")
	}
	start, end := m.window(paneKeys)
	if !(m.cursor[paneKeys] >= start && m.cursor[paneKeys] < end) {
		t.Errorf("cursor %d not in window [%d,%d)", m.cursor[paneKeys], start, end)
	}
	if end-start != cap {
		t.Errorf("window size %d, want %d", end-start, cap)
	}
	if !strings.Contains(m.View(), "rows ") || !strings.Contains(m.View(), "of 40") {
		t.Errorf("expected scroll indicator:\n%s", m.View())
	}
}

func TestPageAndEndKeys(t *testing.T) {
	snap := manyKeysSnap(30)
	m := New(&fakeService{model: snap})
	m = feed(m, tea.WindowSizeMsg{Width: 80, Height: 18})
	m = feed(m, refreshedMsg{model: snap})

	cap := m.listCapacity()
	m = feed(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.cursor[paneKeys] != cap {
		t.Errorf("pgdown cursor = %d, want %d", m.cursor[paneKeys], cap)
	}
	m = feed(m, key("G")) // jump to end
	if m.cursor[paneKeys] != 29 {
		t.Errorf("end cursor = %d, want 29", m.cursor[paneKeys])
	}
	start, end := m.window(paneKeys)
	if end != 30 || m.cursor[paneKeys] < start {
		t.Errorf("end-of-list window wrong: [%d,%d)", start, end)
	}
	m = feed(m, key("g")) // jump home
	if m.cursor[paneKeys] != 0 || m.scroll[paneKeys] != 0 {
		t.Errorf("home not at top: cursor=%d scroll=%d", m.cursor[paneKeys], m.scroll[paneKeys])
	}
}

// TestLayoutFitsHeight guards that the full view never exceeds the terminal
// height (which would scroll the header off the top), with and without a status.
func TestLayoutFitsHeight(t *testing.T) {
	snap := manyKeysSnap(100)
	for _, w := range []int{80, 120} { // narrow (single) and wide (two-column)
		for _, h := range []int{10, 18, 24, 40} {
			m := New(&fakeService{model: snap})
			m = feed(m, tea.WindowSizeMsg{Width: w, Height: h})
			m = feed(m, refreshedMsg{model: snap})
			for _, withStatus := range []bool{false, true} {
				if withStatus {
					m = feed(m, agentDoneMsg{verb: "a status message"})
					m.loading = false
				}
				v := m.View()
				if lines := strings.Count(v, "\n") + 1; lines > h {
					t.Errorf("w=%d h=%d status=%v: view has %d lines (> height)", w, h, withStatus, lines)
				}
				if !strings.Contains(v, "Keys") {
					t.Errorf("w=%d h=%d: header/tabs missing", w, h)
				}
				for _, line := range strings.Split(v, "\n") {
					if lw := lipgloss.Width(line); lw > w {
						t.Errorf("w=%d: line width %d exceeds width", w, lw)
						break
					}
				}
			}
		}
	}
}

func TestFilterNarrowsKeys(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})

	m = feed(m, key("/"))
	if !m.filtering {
		t.Fatal("/ should focus the filter")
	}
	for _, r := range "alp" {
		m = feed(m, key(string(r)))
	}
	vis := m.visibleIDs()
	if len(vis) != 1 || vis[0].Name != "alpha" {
		t.Fatalf("filter 'alp' => %v", vis)
	}
	view := m.View()
	if !strings.Contains(view, "alpha") || strings.Contains(view, "zeta") {
		t.Errorf("filtered view should show alpha only:\n%s", view)
	}

	// enter keeps the query but exits the input.
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.filtering || m.filterQuery() != "alp" {
		t.Errorf("after enter: filtering=%v query=%q", m.filtering, m.filterQuery())
	}
	// selection now indexes the filtered list.
	sel, ok := m.selectedKey()
	if !ok || sel.Name != "alpha" {
		t.Errorf("selectedKey = %v,%v", sel, ok)
	}

	// esc clears the filter.
	m = feed(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.filterQuery() != "" || len(m.visibleIDs()) != 2 {
		t.Errorf("esc should clear filter; query=%q visible=%d", m.filterQuery(), len(m.visibleIDs()))
	}
}

func TestFilterMatchesAlgoAndHosts(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})

	// Filter keys by algorithm.
	m = feed(m, key("/"))
	for _, r := range "rsa" {
		m = feed(m, key(string(r)))
	}
	if vis := m.visibleIDs(); len(vis) != 1 || vis[0].Name != "zeta" {
		t.Errorf("algo filter 'rsa' => %v", vis)
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEsc})

	// Switch to Hosts and filter by hostname.
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab})
	m = feed(m, key("/"))
	for _, r := range "example" {
		m = feed(m, key(string(r)))
	}
	if vis := m.visibleHosts(); len(vis) != 1 || vis[0].Name != "web" {
		t.Errorf("host filter 'example' => %v", vis)
	}
}

func TestFilterClampsCursor(t *testing.T) {
	snap := manyKeysSnap(20)
	m := New(&fakeService{model: snap})
	m = feed(m, tea.WindowSizeMsg{Width: 80, Height: 40})
	m = feed(m, refreshedMsg{model: snap})

	m = feed(m, key("G")) // cursor at last (19)
	m = feed(m, key("/"))
	for _, r := range "id_01" {
		m = feed(m, key(string(r)))
	}
	// Only "id_01" matches; cursor must clamp into range.
	if n := m.rowCountFor(paneKeys); n != 1 {
		t.Fatalf("expected 1 match, got %d", n)
	}
	if m.cursor[paneKeys] != 0 {
		t.Errorf("cursor not clamped after filter: %d", m.cursor[paneKeys])
	}
}

func TestConnectToHostDispatches(t *testing.T) {
	svc := &fakeService{model: snapshot()}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // Hosts pane

	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("enter on a host should dispatch ssh")
	}
	if !strings.Contains(m.status, "connecting to web") {
		t.Errorf("status = %q", m.status)
	}
}

func TestConnectWildcardRejected(t *testing.T) {
	snap := &config.SshConfigModel{
		Hosts: map[config.HostID]config.Host{"*": {ID: "*", Name: "*", IsPattern: true}},
	}
	m := New(&fakeService{model: snap})
	m = feed(m, refreshedMsg{model: snap})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab})

	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd != nil {
		t.Error("should not ssh to a pattern host")
	}
	if !strings.Contains(m.status, "wildcard") {
		t.Errorf("status = %q", m.status)
	}
}

func TestConnectDoneStatus(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	out, _ := m.Update(connectDoneMsg{alias: "web"})
	m = out.(Model)
	if !strings.Contains(m.status, "session to web ended") {
		t.Errorf("status = %q", m.status)
	}
}

func TestFirstAlias(t *testing.T) {
	if got := firstAlias("web prod-web"); got != "web" {
		t.Errorf("firstAlias = %q, want web", got)
	}
	if got := firstAlias(""); got != "" {
		t.Errorf("firstAlias empty = %q", got)
	}
}

func keysAndHostsSnap() *config.SshConfigModel {
	ids := map[config.IdentityID]config.Identity{}
	for _, n := range []string{"a", "b", "c"} {
		ids[config.IdentityID(n)] = config.Identity{ID: config.IdentityID(n), Name: n, ExistsOnDisk: true}
	}
	hosts := map[config.HostID]config.Host{}
	for _, n := range []string{"web0", "web1", "web2", "web3", "prod"} {
		hosts[config.HostID(n)] = config.Host{ID: config.HostID(n), Name: n}
	}
	return &config.SshConfigModel{Identities: ids, Hosts: hosts}
}

func TestPaneSwitchClampsCursorUnderFilter(t *testing.T) {
	snap := keysAndHostsSnap()
	m := New(&fakeService{model: snap})
	m = feed(m, tea.WindowSizeMsg{Width: 80, Height: 40})
	m = feed(m, refreshedMsg{model: snap})

	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // -> Hosts
	m = feed(m, key("G"))                     // cursor at prod (index 4 of 5)
	if m.cursor[paneHosts] != 4 {
		t.Fatalf("setup: hosts cursor = %d, want 4", m.cursor[paneHosts])
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // -> Keys

	// Apply a global filter that leaves only 4 hosts (web0..web3).
	m = feed(m, key("/"))
	for _, r := range "web" {
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // apply, exit filter input

	// Switching back to Hosts must clamp the now-out-of-range cursor.
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab})
	if got := m.cursor[paneHosts]; got != 3 {
		t.Errorf("hosts cursor after switch = %d, want 3 (clamped)", got)
	}
	if h, ok := m.selectedHost(); !ok || h.Name != "web3" {
		t.Errorf("selectedHost = %v,%v, want web3", h, ok)
	}
}

func TestCtrlCQuitsFromFilter(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, key("/")) // focus filter input

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should dispatch quit even while filtering")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c should produce tea.QuitMsg, got %T", cmd())
	}
}

func TestEndKeyOnEmptyList(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	// Filter to zero matches, then jump to end.
	m = feed(m, key("/"))
	for _, r := range "zzzzz" {
		m = feed(m, key(string(r)))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = feed(m, key("G"))
	if m.cursor[paneKeys] != 0 {
		t.Errorf("end on empty list left cursor at %d, want 0", m.cursor[paneKeys])
	}
}

func TestPermsAuditAndFix(t *testing.T) {
	svc := &fakeService{model: snapshot(), permIssues: []perms.Issue{
		{Path: "/home/u/.ssh/id_rsa", Kind: perms.KeyKind, Got: 0o644, Want: 0o600, Why: "too open"},
	}}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})

	m = feed(m, key("P"))
	if m.mode != modePerms {
		t.Fatalf("expected modePerms, got %d", m.mode)
	}
	if !strings.Contains(m.View(), "id_rsa") || !strings.Contains(m.View(), "0600") {
		t.Errorf("perms overlay should list the issue:\n%s", m.View())
	}

	m = feed(m, key("y")) // confirm fix
	if svc.fixedPerms != 1 {
		t.Errorf("FixPermissions called for %d issues, want 1", svc.fixedPerms)
	}
	if m.mode != modeNormal || !strings.Contains(m.status, "fixed permissions") {
		t.Errorf("after fix: mode=%d status=%q", m.mode, m.status)
	}
}

func TestPermsNoIssues(t *testing.T) {
	svc := &fakeService{model: snapshot()} // no issues
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, key("P"))
	if m.mode != modeNormal || !strings.Contains(m.status, "permissions OK") {
		t.Errorf("clean audit: mode=%d status=%q", m.mode, m.status)
	}
}

func TestPermsCancelNoFix(t *testing.T) {
	svc := &fakeService{model: snapshot(), permIssues: []perms.Issue{
		{Path: "/x", Kind: perms.DirKind, Got: 0o755, Want: 0o700},
	}}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, key("P"))
	m = feed(m, key("n")) // cancel
	if svc.fixedPerms != 0 {
		t.Errorf("cancel must not fix: %d", svc.fixedPerms)
	}
	if m.mode != modeNormal {
		t.Errorf("cancel should return to normal: %d", m.mode)
	}
}

func TestKnownHostsBrowseAndRemove(t *testing.T) {
	svc := &fakeService{model: snapshot(), khEntries: []knownhosts.Entry{
		{Hosts: []string{"github.com"}, KeyType: "ssh-ed25519", Fingerprint: "SHA256:aaa", Line: 0},
		{Hosts: []string{"gitlab.com"}, KeyType: "ssh-rsa", Fingerprint: "SHA256:bbb", Line: 1},
	}}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})

	m = feed(m, key("K"))
	if m.mode != modeKnownHosts {
		t.Fatalf("expected modeKnownHosts, got %d", m.mode)
	}
	if !strings.Contains(m.View(), "github.com") || !strings.Contains(m.View(), "gitlab.com") {
		t.Errorf("known_hosts overlay should list entries:\n%s", m.View())
	}

	// Select second entry, request delete, confirm.
	m = feed(m, key("j"))
	if m.khCursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.khCursor)
	}
	m = feed(m, key("d"))
	if !m.khConfirm {
		t.Fatal("d should ask for confirmation")
	}
	m = feed(m, key("y"))
	if len(svc.khRemoved) != 1 || svc.khRemoved[0] != 1 {
		t.Errorf("removed lines = %v, want [1]", svc.khRemoved)
	}
	if len(m.khEntries) != 1 || m.khEntries[0].Hosts[0] != "github.com" {
		t.Errorf("list not reloaded after remove: %+v", m.khEntries)
	}
}

func TestKnownHostsEmpty(t *testing.T) {
	m := New(&fakeService{model: snapshot()}) // no entries
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, key("K"))
	if m.mode != modeNormal || !strings.Contains(m.status, "no known_hosts") {
		t.Errorf("empty known_hosts: mode=%d status=%q", m.mode, m.status)
	}
}

func TestKnownHostsCancelDelete(t *testing.T) {
	svc := &fakeService{model: snapshot(), khEntries: []knownhosts.Entry{
		{Hosts: []string{"github.com"}, KeyType: "ssh-ed25519", Line: 0},
	}}
	m := New(svc)
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, key("K"))
	m = feed(m, key("d"))
	m = feed(m, key("n")) // cancel confirm
	if m.khConfirm {
		t.Error("n should cancel the confirm")
	}
	if len(svc.khRemoved) != 0 {
		t.Errorf("cancel must not remove: %v", svc.khRemoved)
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEsc}) // close
	if m.mode != modeNormal {
		t.Error("esc should close the overlay")
	}
}

func TestCopyKeyPublicAndFingerprint(t *testing.T) {
	var copied string
	restore := clip.SetWriter(func(s string) error { copied = s; return nil })
	defer restore()

	snap := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"id_ed": {ID: "id_ed", Name: "id_ed", ExistsOnDisk: true,
				PublicKey: "ssh-ed25519 AAAAC3Nz me@host", Fingerprint: "SHA256:abc"},
		},
	}
	m := New(&fakeService{model: snap})
	m = feed(m, refreshedMsg{model: snap})

	m = feed(m, key("c"))
	if m.mode != modeCopy {
		t.Fatalf("expected modeCopy, got %d", m.mode)
	}
	v := m.View()
	if !strings.Contains(v, "public key") || !strings.Contains(v, "fingerprint") {
		t.Errorf("copy menu missing options:\n%s", v)
	}
	out, cmd := m.Update(key("p"))
	m = out.(Model)
	m = feed(m, cmd()) // executes the copy + delivers clipDoneMsg
	if copied != "ssh-ed25519 AAAAC3Nz me@host" {
		t.Errorf("copied %q", copied)
	}
	if !strings.Contains(m.status, "copied public key") {
		t.Errorf("status = %q", m.status)
	}
}

func TestCopyHostSshCommand(t *testing.T) {
	var copied string
	restore := clip.SetWriter(func(s string) error { copied = s; return nil })
	defer restore()

	m := New(&fakeService{model: snapshot()}) // host web: deploy@example.com:22
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // Hosts pane

	m = feed(m, key("c"))
	out, cmd := m.Update(key("s"))
	m = out.(Model)
	cmd()
	if copied != "ssh deploy@example.com -p 22" {
		t.Errorf("copied %q", copied)
	}
}

func TestSshCommand(t *testing.T) {
	if got := sshCommand(config.Host{Name: "web", Hostname: "h", User: "u", Port: 2222}); got != "ssh u@h -p 2222" {
		t.Errorf("explicit = %q", got)
	}
	if got := sshCommand(config.Host{Name: "alias only"}); got != "ssh alias" {
		t.Errorf("alias fallback = %q", got)
	}
}

func TestHelpOverlay(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})

	m = feed(m, key("?"))
	if m.mode != modeHelp {
		t.Fatalf("? should open help, got mode %d", m.mode)
	}
	v := m.View()
	for _, want := range []string{"keybindings", "Navigation", "Keys pane", "Hosts pane", "known_hosts"} {
		if !strings.Contains(v, want) {
			t.Errorf("help missing %q:\n%s", want, v)
		}
	}
	// Any key closes it.
	m = feed(m, key("x"))
	if m.mode != modeNormal {
		t.Errorf("a key should close help, got mode %d", m.mode)
	}
}

func TestHelpFitsAndScrolls(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, tea.WindowSizeMsg{Width: 72, Height: 14}) // shorter than the help
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, key("?"))

	// Rendered output must never exceed the terminal height (no clipping).
	for _, n := range []int{0, 3, 100} { // top, mid, past-bottom
		mm := m
		for i := 0; i < n; i++ {
			out, _ := mm.Update(tea.KeyMsg{Type: tea.KeyDown})
			mm = out.(Model)
		}
		if lines := strings.Count(mm.View(), "\n") + 1; lines > 14 {
			t.Errorf("scroll=%d: help is %d lines, exceeds height 14", n, lines)
		}
		if !strings.Contains(mm.View(), "scroll") {
			t.Errorf("scroll=%d: overflowing help should show a scroll hint", n)
		}
	}

	// Scrolling down reveals later sections.
	for i := 0; i < 100; i++ {
		out, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = out.(Model)
	}
	if !strings.Contains(m.View(), "delete directive") {
		t.Errorf("scrolling to the bottom should reveal the last rows:\n%s", m.View())
	}
	// A non-scroll key still closes.
	m = feed(m, key("x"))
	if m.mode != modeNormal {
		t.Error("non-scroll key should close help")
	}
}

func TestSinglePaneShowsActiveOnly(t *testing.T) {
	snap := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"keyonly": {ID: "keyonly", Name: "keyonly", ExistsOnDisk: true},
		},
		Hosts: map[config.HostID]config.Host{
			"hostonly": {ID: "hostonly", Name: "hostonly", Hostname: "h.example"},
		},
	}
	// Even on a wide terminal, only the active pane is shown (no two-column).
	m := New(&fakeService{model: snap})
	m = feed(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	m = feed(m, refreshedMsg{model: snap})
	v := m.View()
	if !strings.Contains(v, "keyonly") || strings.Contains(v, "hostonly") {
		t.Errorf("keys pane should show only keys:\n%s", v)
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab}) // -> hosts
	v = m.View()
	if !strings.Contains(v, "hostonly") || strings.Contains(v, "keyonly") {
		t.Errorf("hosts pane should show only hosts:\n%s", v)
	}
}

func TestRowTruncationInColumn(t *testing.T) {
	snap := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"k": {ID: "k", Name: "k", ExistsOnDisk: true,
				Comment: "this is a very long comment that will overflow a narrow column for sure"},
		},
	}
	m := New(&fakeService{model: snap})
	m = feed(m, tea.WindowSizeMsg{Width: 100, Height: 20}) // wide → ~46-col columns
	m = feed(m, refreshedMsg{model: snap})
	v := m.View()
	if !strings.Contains(v, "…") {
		t.Errorf("long row should be truncated with an ellipsis:\n%s", v)
	}
	// No rendered line should exceed the terminal width.
	for _, line := range strings.Split(v, "\n") {
		if w := lipgloss.Width(line); w > 100 {
			t.Errorf("line width %d exceeds 100: %q", w, line)
		}
	}
}

func TestMotionOffNoEffect(t *testing.T) {
	m := New(&fakeService{model: snapshot()}).WithSettings(&fakeSettings{}) // motion off
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, agentDoneMsg{verb: "key loaded"})
	if m.fxActive() {
		t.Error("no effect should play when motion is disabled")
	}
}

func TestMotionFlashOnAction(t *testing.T) {
	fs := &fakeSettings{motionOn: true}
	m := New(&fakeService{model: snapshot()}).WithSettings(fs)
	m = feed(m, refreshedMsg{model: snapshot()})

	out, cmd := m.Update(agentDoneMsg{verb: "key loaded"})
	m = out.(Model)
	if !m.fx.flashGood || !m.fxActive() {
		t.Errorf("success should flash good; fx=%+v", m.fx)
	}
	if cmd == nil {
		t.Error("a frame ticker should be scheduled")
	}

	// An error flashes bad and shakes.
	out, _ = m.Update(agentDoneMsg{verb: "x", err: errFake{}})
	m = out.(Model)
	if !m.fx.flashBad || m.fx.shakeAmp == 0 {
		t.Errorf("error should flash bad + shake; fx=%+v", m.fx)
	}
}

func TestMotionIntensityScalesShake(t *testing.T) {
	for _, c := range []struct {
		level string
		amp   int
	}{{"subtle", 2}, {"normal", 4}, {"arcade", 7}} {
		fs := &fakeSettings{motionOn: true, motionLevel: c.level}
		m := New(&fakeService{model: snapshot()}).WithSettings(fs)
		out, _ := m.Update(agentDoneMsg{verb: "x", err: errFake{}}) // error → shakes
		m = out.(Model)
		if m.fx.shakeAmp != c.amp {
			t.Errorf("%s: shakeAmp=%d, want %d", c.level, m.fx.shakeAmp, c.amp)
		}
	}
}

func TestMotionFrameExpires(t *testing.T) {
	fs := &fakeSettings{motionOn: true}
	m := New(&fakeService{model: snapshot()}).WithSettings(fs)
	m.fx = activeFX{shakeAmp: 3, start: time.Now().Add(-time.Second), dur: 200 * time.Millisecond} // already expired
	out, cmd := m.Update(frameMsg{})
	m = out.(Model)
	if m.fx.any() {
		t.Error("expired effect should be cleared")
	}
	if cmd != nil {
		t.Error("no more frames once the effect ends")
	}
}

func TestToggleMotionPersists(t *testing.T) {
	fs := &fakeSettings{}
	m := New(&fakeService{model: snapshot()}).WithSettings(fs)
	m = feed(m, refreshedMsg{model: snapshot()})

	m = feed(m, key("m"))
	if !fs.motionOn || !strings.Contains(m.status, "motion on") {
		t.Errorf("m should enable motion; on=%v status=%q", fs.motionOn, m.status)
	}
	m = feed(m, key("m"))
	if fs.motionOn || !strings.Contains(m.status, "motion off") {
		t.Errorf("second m should disable; on=%v status=%q", fs.motionOn, m.status)
	}
}

func TestThemeApplyOnSettings(t *testing.T) {
	defer applyTheme(defaultTheme)
	applyTheme(defaultTheme)
	if colPrimary != defaultTheme.Primary {
		t.Fatalf("setup: primary=%v", colPrimary)
	}
	_ = New(&fakeService{model: snapshot()}).WithSettings(&fakeSettings{themeName: "dracula"})
	drac, _ := themeByName("dracula")
	if colPrimary != drac.Primary {
		t.Errorf("saved theme not applied: primary=%v want %v", colPrimary, drac.Primary)
	}
}

func TestThemeSwitcherApply(t *testing.T) {
	defer applyTheme(defaultTheme)
	fs := &fakeSettings{}
	m := New(&fakeService{model: snapshot()}).WithSettings(fs)
	m = feed(m, refreshedMsg{model: snapshot()})

	m = feed(m, key("t"))
	if m.mode != modeTheme {
		t.Fatalf("t should open theme picker, got %d", m.mode)
	}
	if v := m.View(); !strings.Contains(v, "dracula") || !strings.Contains(v, "nord") {
		t.Errorf("theme picker should list presets:\n%s", v)
	}
	// Move to the 2nd preset and apply.
	m = feed(m, key("j"))
	want := presets[1].name
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter})
	if fs.themeName != want || m.mode != modeNormal {
		t.Errorf("apply: themeName=%q mode=%d, want %q/normal", fs.themeName, m.mode, want)
	}
}

func TestThemeSwitcherCancelReverts(t *testing.T) {
	defer applyTheme(defaultTheme)
	fs := &fakeSettings{themeName: "nord"}
	m := New(&fakeService{model: snapshot()}).WithSettings(fs) // applies nord
	m = feed(m, refreshedMsg{model: snapshot()})
	nord, _ := themeByName("nord")

	m = feed(m, key("t"))
	m = feed(m, key("j")) // preview a different theme (changes global styles)
	if colPrimary == nord.Primary {
		t.Fatal("preview should have changed the live theme")
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEsc}) // cancel
	if fs.themeName != "nord" {
		t.Errorf("cancel must not persist: %q", fs.themeName)
	}
	if colPrimary != nord.Primary {
		t.Errorf("cancel should revert the live theme to nord")
	}
}

// draculaIdx is the presets index of a theme that sets a background.
func draculaIdx() int {
	for i, p := range presets {
		if p.name == "dracula" {
			return i
		}
	}
	return 0
}

func TestBackgroundFillsWholeScreen(t *testing.T) {
	prof := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(prof)
	defer applyTheme(defaultTheme)
	lipgloss.SetColorProfile(termenv.TrueColor)
	applyTheme(presets[draculaIdx()].theme) // has Bg

	out := applyBackground("hi\nthere", 10, 6)
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("want 6 rows filled to height, got %d", len(lines))
	}
	bg := bgOpenSeq()
	for i, ln := range lines {
		if !strings.HasPrefix(ln, bg) {
			t.Errorf("row %d missing bg prefix: %q", i, ln)
		}
		if w := lipgloss.Width(ln); w != 10 {
			t.Errorf("row %d width=%d, want 10 (padded, no wrap)", i, w)
		}
	}
}

func TestBackgroundNoColorEmitsNoEscape(t *testing.T) {
	prof := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(prof)
	defer applyTheme(defaultTheme)
	lipgloss.SetColorProfile(termenv.Ascii) // NO_COLOR / no-color terminal
	applyTheme(presets[draculaIdx()].theme) // bg theme, but ascii strips color

	out := applyBackground("hi", 10, 3)
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("ascii profile must stay escape-free, got %q", out)
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
