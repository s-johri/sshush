package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// restoreOverlay is the restore-from-backup y/n gate. It is stateless — the
// files to revert are read from the service at render time.
type restoreOverlay struct{}

func (o *restoreOverlay) Update(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	if msg.String() != "y" && msg.String() != "Y" {
		m.status = "restore cancelled"
		return nil, nil
	}
	return nil, func() tea.Msg {
		files, err := m.svc.RestoreBackup()
		return restoreDoneMsg{files: files, err: err}
	}
}

func (o *restoreOverlay) View(m *Model) string {
	var b strings.Builder
	b.WriteString(errStyle.Render("Restore config from backup") + "\n\n")
	b.WriteString(textStyle.Render("  Revert these file(s) to their .bak snapshot (taken before") + "\n")
	b.WriteString(textStyle.Render("  sshush's first edit), discarding changes made since:") + "\n\n")
	for _, p := range m.svc.BackupPaths() {
		b.WriteString("  " + textStyle.Render(p) + dimStyle.Render(".bak → "+p) + "\n")
	}
	b.WriteString("\n  " + keyCap.Render("y") + textStyle.Render(" restore    ") + keyCap.Render("n") + textStyle.Render(" cancel"))
	b.WriteString("\n")
	return b.String()
}
