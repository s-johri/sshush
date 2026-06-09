package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/pkg/config"
)

// pickerOverlay attaches/detaches keys to a host via IdentityFile. enter toggles
// the highlighted key's association; the overlay stays open for more changes
// (the dispatched editDoneMsg refreshes the model underneath it).
type pickerOverlay struct {
	host   config.HostID
	cursor int
}

func (o *pickerOverlay) Update(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	disk := m.diskKeys()
	switch msg.String() {
	case "esc", "q":
		return nil, nil
	case "up", "k":
		if o.cursor > 0 {
			o.cursor--
		}
	case "down", "j":
		if o.cursor < len(disk)-1 {
			o.cursor++
		}
	case "enter", " ":
		if o.cursor >= len(disk) {
			return o, nil
		}
		host, ok := m.hostByID(o.host)
		if !ok {
			return nil, nil
		}
		sel := disk[o.cursor]
		h := o.host
		if hostHasIdentity(host, sel.ID) {
			return o, func() tea.Msg { return editDoneMsg{verb: "key detached", err: m.svc.DetachKey(h, sel.ID)} }
		}
		return o, func() tea.Msg { return editDoneMsg{verb: "key attached", err: m.svc.AttachKey(h, sel.ID)} }
	}
	return o, nil
}

// View renders the disk keys with a filled glyph for those associated with the
// host via IdentityFile.
func (o *pickerOverlay) View(m *Model) string {
	host, _ := m.hostByID(o.host)
	disk := m.diskKeys()
	var b strings.Builder
	b.WriteString(tabActive.Render("Keys for host: "+string(o.host)) + "\n\n")
	for i, id := range disk {
		attached := hostHasIdentity(host, id.ID)
		glyph := glyphUnloaded
		glyphStyle := dimStyle
		if attached {
			glyph, glyphStyle = glyphLoaded, loadedBadge
		}
		if i == o.cursor {
			b.WriteString(selectedRow.Render("▸ "+glyph+" "+id.Name) + "\n")
		} else {
			b.WriteString("  " + glyphStyle.Render(glyph) + " " + id.Name + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  ↑/↓ move · enter attach/detach · esc close"))
	b.WriteString("\n")
	return b.String()
}
