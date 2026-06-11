package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/pkg/clip"
)

// copyOverlay is the clipboard copy menu: pick an option by its hotkey to copy
// it. Built by beginCopy with the options for the active pane.
type copyOverlay struct {
	opts []copyOption
}

func (o *copyOverlay) Update(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	switch msg.String() {
	case "esc", "c":
		return nil, nil
	}
	for _, opt := range o.opts {
		if msg.String() == opt.key {
			label, content := opt.label, opt.content
			return nil, func() tea.Msg { return clipDoneMsg{label: label, err: clip.Write(content)} }
		}
	}
	return o, nil
}

func (o *copyOverlay) View(m *Model) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Copy to clipboard") + "\n\n")
	for _, opt := range o.opts {
		preview := opt.content
		if len(preview) > 48 {
			preview = preview[:48] + "…"
		}
		b.WriteString("  " + keyCap.Render(opt.key) + "  " + opt.label + "  " + dimStyle.Render(preview) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("  press a letter · esc cancel") + "\n")
	return b.String()
}
