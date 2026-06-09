package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/pkg/clip"
)

// copyOverlay is exercised directly through its overlay interface — no full
// Model/Update loop needed.
func TestCopyOverlayInIsolation(t *testing.T) {
	o := &copyOverlay{opts: []copyOption{
		{"p", "public key", "ssh-ed25519 AAAA"},
		{"f", "fingerprint", "SHA256:xyz"},
	}}
	var m Model

	if v := o.View(&m); !strings.Contains(v, "public key") || !strings.Contains(v, "fingerprint") {
		t.Errorf("view missing options:\n%s", v)
	}

	// Unknown key: stays open, no command.
	if next, cmd := o.Update(key("z"), &m); next != overlay(o) || cmd != nil {
		t.Errorf("unknown key should stay open: next=%v cmd=%v", next, cmd)
	}

	// esc closes with no command.
	if next, cmd := o.Update(tea.KeyMsg{Type: tea.KeyEsc}, &m); next != nil || cmd != nil {
		t.Errorf("esc should close: next=%v cmd=%v", next, cmd)
	}

	// Picking an option closes and emits a copy command.
	var copied string
	restore := clip.SetWriter(func(s string) error { copied = s; return nil })
	defer restore()
	next, cmd := o.Update(key("f"), &m)
	if next != nil || cmd == nil {
		t.Fatalf("picking should close + emit a cmd: next=%v cmd=%v", next, cmd)
	}
	msg, ok := cmd().(clipDoneMsg)
	if !ok || msg.label != "fingerprint" || copied != "SHA256:xyz" {
		t.Errorf("copy cmd wrong: msg=%+v copied=%q", msg, copied)
	}
}
