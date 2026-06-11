package tui

import tea "github.com/charmbracelet/bubbletea"

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
	Update(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd)
	View(m *Model) string
}
