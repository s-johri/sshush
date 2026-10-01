package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// restoreOverlay is the restore-from-backup y/n gate. It is stateless — the
// files to revert are read from the service at render time.
type restoreOverlay struct{}

func (o *restoreOverlay) Update(msg tea.KeyPressMsg, m *Model) (overlay, tea.Cmd) {
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
	b.WriteString(textStyle.Render("  Write these backups over the current files. The current content") + "\n")
	b.WriteString(textStyle.Render("  of each file is saved first, next to its backup.") + "\n\n")
	for _, bk := range m.svc.Backups() {
		b.WriteString("  " + textStyle.Render(bk.File) +
			dimStyle.Render("  backup from "+bk.ModTime.Format("2006-01-02 15:04")) + "\n")
	}
	b.WriteString("\n  " + keyCap.Render("y") + textStyle.Render(" restore    ") + keyCap.Render("n") + textStyle.Render(" cancel"))
	b.WriteString("\n")
	return b.String()
}
