package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// overlay is a modal screen layered over the panes. Update handles one key,
// returning the overlay to show next — itself (stay open), a successor, or nil
// (close, back to the panes) — plus an optional command. View renders its body;
// the caller frames it with a card.
//
// Overlays own their own working state (cursor, input, options, phase). They
// reach the Model only for shared concerns: services (svc, settings), the
// status line, the current selection, and terminal size. Commands they emit
// (e.g. clipDoneMsg, editDoneMsg) flow back to the main Update, which owns the
// async result + refresh — overlays fire and close, never receiving DoneMsgs.
// See CONTEXT.md.
type overlay interface {
	Update(msg tea.KeyPressMsg, m *Model) (overlay, tea.Cmd)
	View(m *Model) string
}

// styleTextInput paints an input's prompt and text with the theme body style.
// bubbles v2 keeps separate focused and blurred style states, so both get the
// same style, which is what the single v1 style did.
func styleTextInput(ti *textinput.Model) {
	s := ti.Styles()
	s.Focused.Prompt, s.Focused.Text = textStyle, textStyle
	s.Blurred.Prompt, s.Blurred.Text = textStyle, textStyle
	ti.SetStyles(s)
}
