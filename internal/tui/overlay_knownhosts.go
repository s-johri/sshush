package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/pkg/knownhosts"
)

// knownHostsOverlay browses ~/.ssh/known_hosts and removes entries (confirm-
// gated). It owns a viewport over the entries plus an inline confirm sub-state.
type knownHostsOverlay struct {
	entries []knownhosts.Entry
	vp      viewport
	confirm bool // confirming removal of the selected entry
}

// capacity is the visible-row budget for the overlay given the terminal height.
func (o *knownHostsOverlay) capacity(m *Model) int {
	if m.height <= 0 {
		return 1 << 30
	}
	if c := m.height - 9; c > 1 {
		return c
	}
	return 1
}

func (o *knownHostsOverlay) Update(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	if o.confirm {
		if msg.String() == "y" || msg.String() == "Y" {
			return o.remove(m)
		}
		o.confirm = false
		return o, nil
	}
	switch msg.String() {
	case "esc", "q":
		return nil, nil
	case "up", "k":
		o.vp.moveCursor(-1, len(o.entries), o.capacity(m))
	case "down", "j":
		o.vp.moveCursor(1, len(o.entries), o.capacity(m))
	case "d", "enter":
		if len(o.entries) > 0 {
			o.confirm = true
		}
	}
	return o, nil
}

// remove deletes the selected entry and reloads the list; it closes the overlay
// on error or when no entries remain.
func (o *knownHostsOverlay) remove(m *Model) (overlay, tea.Cmd) {
	ent := o.entries[o.vp.cursor]
	o.confirm = false
	if err := m.svc.RemoveKnownHost(ent.Line); err != nil {
		m.status = "remove failed: " + err.Error()
		return nil, nil
	}
	entries, _ := m.svc.KnownHosts()
	o.entries = entries
	o.vp.clampCursor(len(entries))
	o.vp.ensureVisible(len(entries), o.capacity(m))
	m.status = "removed known_hosts entry (backup written)"
	if len(entries) == 0 {
		return nil, nil
	}
	return o, nil
}

func (o *knownHostsOverlay) View(m *Model) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("known_hosts — %d entries", len(o.entries))) + "\n\n")

	start, end := o.vp.window(len(o.entries), o.capacity(m))
	for i := start; i < end; i++ {
		e := o.entries[i]
		// Pad the plain host/keytype cells first (so column math counts runes,
		// not ANSI bytes), then route them through a theme style.
		hostCell := fmt.Sprintf("%-28s", e.Display())
		typeCell := fmt.Sprintf("%-20s ", e.KeyType)
		hostStyle := textStyle
		if e.Hashed {
			hostStyle = dimStyle
		}
		line := hostStyle.Render(hostCell) + " " + textStyle.Render(typeCell) + dimStyle.Render(e.Fingerprint)
		if i == o.vp.cursor {
			b.WriteString(selectedRow.Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	if start > 0 || end < len(o.entries) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  rows %d–%d of %d\n", start+1, end, len(o.entries))))
	}

	b.WriteString("\n")
	if o.confirm {
		sel := o.entries[o.vp.cursor]
		b.WriteString(errStyle.Render(fmt.Sprintf("  remove key for %s?  ", sel.Display())) +
			keyCap.Render("y") + textStyle.Render(" yes  ") + keyCap.Render("n") + textStyle.Render(" no"))
	} else {
		b.WriteString(dimStyle.Render("  ↑/↓ move · d remove · esc close"))
	}
	b.WriteString("\n")
	return b.String()
}
