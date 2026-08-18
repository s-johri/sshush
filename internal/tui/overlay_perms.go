package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/s-johri/sshush/pkg/perms"
)

// permsOverlay lists permission issues found by the audit and gates the chmod
// fix behind y/n.
type permsOverlay struct {
	issues []perms.Issue
}

func (o *permsOverlay) Update(msg tea.KeyPressMsg, m *Model) (overlay, tea.Cmd) {
	if msg.String() != "y" && msg.String() != "Y" {
		m.status = "permissions unchanged"
		return nil, nil
	}
	n := len(o.issues)
	if err := m.svc.FixPermissions(o.issues); err != nil {
		m.status = "fix failed: " + err.Error()
		return nil, nil
	}
	m.status = fmt.Sprintf("fixed permissions on %d file(s)", n)
	return nil, nil
}

func (o *permsOverlay) View(m *Model) string {
	var b strings.Builder
	b.WriteString(errStyle.Render("Permission issues — ssh may reject these") + "\n\n")
	for _, i := range o.issues {
		b.WriteString("  " + textStyle.Render(i.Path) + "  " +
			dimStyle.Render(fmt.Sprintf("%04o", i.Got)) +
			textStyle.Render("→") +
			starStyle.Render(fmt.Sprintf("%04o", i.Want)) + "  " +
			dimStyle.Render(i.Why) + "\n")
	}
	b.WriteString("\n  " + keyCap.Render("y") + textStyle.Render(" fix all (chmod)    ") + keyCap.Render("n") + textStyle.Render(" cancel"))
	b.WriteString("\n")
	return b.String()
}
