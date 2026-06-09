package tui

import (
	"fmt"
	"math/rand"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// themeOverlay is the live theme picker: ↑/↓ preview, r randomize, enter applies
// + persists, esc reverts to the theme that was active when it opened. Preview
// mutates the package-level styles via applyTheme (see candidate 04 / theme.go).
type themeOverlay struct {
	cursor int
	orig   string // theme name to revert to on cancel
}

func (o *themeOverlay) Update(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if o.cursor > 0 {
			o.cursor--
		}
	case "down", "j":
		if o.cursor < len(presets)-1 {
			o.cursor++
		}
	case "r":
		o.cursor = rand.Intn(len(presets))
	case "enter":
		name := presets[o.cursor].name
		if err := m.settings.SetTheme(name); err != nil {
			m.status = "theme applied (not saved): " + err.Error()
			return nil, nil
		}
		m.status = "theme: " + name
		return nil, nil
	case "esc", "q":
		t, ok := themeByName(o.orig)
		if !ok {
			t = defaultTheme
		}
		applyTheme(t)
		m.status = "theme unchanged"
		return nil, nil
	}
	applyTheme(presets[o.cursor].theme) // live preview
	return o, nil
}

func (o *themeOverlay) View(m *Model) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Theme") + "\n\n")

	// Window the list around the cursor so it never overflows a short terminal.
	cap := len(presets)
	if m.height > 0 {
		if c := m.height - 12; c > 2 {
			cap = c
		} else {
			cap = 2
		}
	}
	// Centre the cursor in the window (clampWindow handles the edges).
	start, end := clampWindow(o.cursor-cap/2, len(presets), cap)
	for i := start; i < end; i++ {
		if i == o.cursor {
			b.WriteString(selectedRow.Render("▸ "+presets[i].name) + "\n")
		} else {
			b.WriteString("  " + textStyle.Render(presets[i].name) + "\n")
		}
	}
	if start > 0 || end < len(presets) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  %d–%d of %d", start+1, end, len(presets))) + "\n")
	}
	// Preview swatch using the live styles.
	swatch := loadedBadge.Render("●") + " " + textStyle.Render("loaded") + "   " +
		starStyle.Render("★ default") + "   " + hostTagStyle.Render("↪ host") + "   " +
		errStyle.Render("error")
	b.WriteString("\n  " + swatch + "\n")
	b.WriteString("\n" + dimStyle.Render("  ↑/↓ preview · enter apply · r random · esc cancel") + "\n")
	return b.String()
}
