package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The tests in this file guard T3: bubbletea v2 sends a paste as one
// tea.PasteMsg, not as key presses, so each text input must handle it.

func paste(s string) tea.PasteMsg { return tea.PasteMsg{Content: s} }

func TestPasteIntoFilter(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, key("/"))
	m = feed(m, paste("alp"))
	if got := m.filterInput.Value(); got != "alp" {
		t.Fatalf("filter = %q, want %q", got, "alp")
	}
	if vis := m.visibleIDs(); len(vis) != 1 || vis[0].Name != "alpha" {
		t.Errorf("paste did not re-filter: %v", vis)
	}
}

// TestPasteIgnoredWithoutInput: with no input open, a paste changes nothing.
func TestPasteIgnoredWithoutInput(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, paste("alp"))
	if got := m.filterInput.Value(); got != "" {
		t.Errorf("filter = %q, want empty", got)
	}
}

func TestPasteIntoEditValue(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyPressMsg{Code: tea.KeyTab}) // Hosts pane
	m = feed(m, key("e"))
	o := m.modal.(*editOverlay)
	before := o.input.Value()
	m = feed(m, paste("XYZ"))
	if got := m.modal.(*editOverlay).input.Value(); got != before+"XYZ" {
		t.Errorf("value = %q, want %q", got, before+"XYZ")
	}
}

func TestPasteIntoEditOptionName(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = feed(m, key("e"))
	m = feed(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if o := m.modal.(*editOverlay); o.phase != edPhaseOptName {
		t.Fatalf("phase = %d, want option name", o.phase)
	}
	m = feed(m, paste("ForwardAgent"))
	if got := m.modal.(*editOverlay).input.Value(); got != "ForwardAgent" {
		t.Errorf("option name = %q", got)
	}
}

// TestPasteIgnoredInEditConfirm: on the y/n step, a paste must not change
// the value that the user is about to confirm.
func TestPasteIgnoredInEditConfirm(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = feed(m, key("e"))
	m = feed(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	o := m.modal.(*editOverlay)
	if o.phase != edPhaseConfirm {
		t.Fatalf("phase = %d, want confirm", o.phase)
	}
	before := o.input.Value()
	m = feed(m, paste("XYZ"))
	if got := m.modal.(*editOverlay).input.Value(); got != before {
		t.Errorf("value = %q after paste on confirm, want %q", got, before)
	}
}

func TestPasteIntoNewHostWizard(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = feed(m, key("n"))
	m = feed(m, paste("db-prod"))
	if got := m.modal.(*newHostWizard).input.Value(); got != "db-prod" {
		t.Errorf("alias = %q", got)
	}
}

func TestPasteIntoNewKeyWizard(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, key("n"))
	// Algorithm step: no input, so the paste changes nothing.
	m = feed(m, paste("junk"))
	if w := m.modal.(*newKeyWizard); w.input.Value() != "" || w.phase != nkPhaseAlgo {
		t.Fatalf("paste on the algorithm step: value %q, phase %d", w.input.Value(), w.phase)
	}
	m = feed(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // ed25519 -> name step
	m = feed(m, paste("_work"))
	if got := m.modal.(*newKeyWizard).input.Value(); got != "id_ed25519_work" {
		t.Errorf("name = %q", got)
	}
	m = feed(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // -> comment step
	m = feed(m, paste("@laptop"))
	if got := m.modal.(*newKeyWizard).input.Value(); got != "id_ed25519_work@laptop" {
		t.Errorf("comment = %q", got)
	}
}
