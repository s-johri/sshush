package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// helpOverlay shows the full keybinding reference, scrolling when it is taller
// than the terminal. It scrolls without a cursor, so it holds a plain offset and
// shares only clampWindow with the pane viewport.
type helpOverlay struct {
	scroll int
}

// capacity is how many body rows fit in the help card for the terminal height
// (card margin + borders + title/blank/footer chrome). Large when unknown.
func (o *helpOverlay) capacity(m *Model) int {
	if m.height <= 0 {
		return 1 << 30
	}
	if c := m.height - 8; c > 1 {
		return c
	}
	return 1
}

// maxScroll is the furthest the body can scroll.
func (o *helpOverlay) maxScroll(m *Model) int {
	if n := len(m.helpLines()) - o.capacity(m); n > 0 {
		return n
	}
	return 0
}

// Update scrolls; any non-scroll key closes the overlay.
func (o *helpOverlay) Update(msg tea.KeyPressMsg, m *Model) (overlay, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		o.scroll--
	case "down", "j":
		o.scroll++
	case "pgup", "ctrl+u":
		o.scroll -= 5
	case "pgdown", "ctrl+d":
		o.scroll += 5
	default:
		return nil, nil
	}
	// Clamp to the real range so scrolling back up responds immediately at the
	// bottom (no overshoot/lag).
	if max := o.maxScroll(m); o.scroll > max {
		o.scroll = max
	}
	if o.scroll < 0 {
		o.scroll = 0
	}
	return o, nil
}

func (o *helpOverlay) View(m *Model) string {
	lines := m.helpLines()
	start, end := clampWindow(o.scroll, len(lines), o.capacity(m))

	var b strings.Builder
	b.WriteString(titleStyle.Render("sshush — keybindings") + "\n\n")
	b.WriteString(strings.Join(lines[start:end], "\n"))
	b.WriteString("\n\n")
	if start > 0 || end < len(lines) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  %d–%d of %d · ↑/↓ scroll · any other key close",
			start+1, end, len(lines))))
	} else {
		b.WriteString(dimStyle.Render("  press any key to close"))
	}
	return b.String()
}
