package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/pkg/config"
)

type fakeService struct {
	model *config.SshConfigModel
	err   error
}

func (f fakeService) Refresh() (*config.SshConfigModel, error)     { return f.model, f.err }
func (f fakeService) AddKeyToAgent(config.IdentityID) error        { return nil }
func (f fakeService) RemoveKeyFromAgent(config.IdentityID) error   { return nil }
func (f fakeService) EditHost(config.HostID, string, string) error { return nil }
func (f fakeService) AddHost(config.Host) error                    { return nil }
func (f fakeService) DeleteHost(config.HostID) error               { return nil }

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
	m := New(fakeService{model: snapshot()})
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
	m := New(fakeService{model: snapshot()})
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
	m := New(fakeService{model: snap})
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
	m := New(fakeService{model: snapshot()})
	out, cmd := m.Update(agentDoneMsg{verb: "loaded"})
	m = out.(Model)
	if cmd == nil {
		t.Error("successful agent action should trigger a refresh")
	}
	if !strings.Contains(m.status, "loaded") {
		t.Errorf("status = %q", m.status)
	}
}

func TestRefreshErrorShown(t *testing.T) {
	m := New(fakeService{})
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
