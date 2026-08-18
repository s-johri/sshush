package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/s-johri/sshush/pkg/config"
)

// deleteConfirmOverlay is the y/n gate for deleting the selected host (config
// block) or key (files on disk — irreversible). Exactly one of host/key is set.
type deleteConfirmOverlay struct {
	host config.HostID     // host to remove, when deleting a host
	key  config.IdentityID // key to delete, when deleting a key's files
}

func (o *deleteConfirmOverlay) Update(msg tea.KeyPressMsg, m *Model) (overlay, tea.Cmd) {
	if msg.String() != "y" && msg.String() != "Y" {
		m.status = "deletion cancelled"
		return nil, nil
	}
	m.status = "deleting…"
	shake := m.play(true, true) // destructive: flash + a forced shake at any level
	if o.key != "" {
		id := o.key
		return nil, tea.Batch(shake, func() tea.Msg { return editDoneMsg{verb: "key deleted", err: m.svc.DeleteKey(id)} })
	}
	h := o.host
	return nil, tea.Batch(shake, func() tea.Msg { return editDoneMsg{verb: "host removed", err: m.svc.DeleteHost(h)} })
}

func (o *deleteConfirmOverlay) View(m *Model) string {
	var b strings.Builder
	if o.key != "" {
		b.WriteString(errStyle.Render("Delete key files — IRREVERSIBLE") + "\n\n")
		b.WriteString(textStyle.Render(fmt.Sprintf("  Permanently delete %s and its .pub from disk", o.key)) + "\n")
		b.WriteString(errStyle.Render("  the private key cannot be recovered") + "\n\n")
	} else {
		b.WriteString(errStyle.Render("Delete host") + "\n\n")
		b.WriteString(textStyle.Render(fmt.Sprintf("  Remove host %s from the config", o.host)) + "\n")
		if h, ok := m.hostByID(o.host); ok && h.IsPattern {
			b.WriteString(errStyle.Render("  this is a wildcard block — removes defaults for every matching connection") + "\n")
		}
		b.WriteString(dimStyle.Render("  (a .bak backup of the config file is written first)") + "\n\n")
	}
	b.WriteString("  " + keyCap.Render("y") + textStyle.Render(" delete    ") + keyCap.Render("n") + textStyle.Render(" cancel"))
	b.WriteString("\n")
	return b.String()
}
