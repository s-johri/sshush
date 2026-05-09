package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// model holds the application state
type model struct {
	input   textinput.Model
	history []string
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Type something and press Enter..."
	ti.Focus()
	ti.CharLimit = 200
	ti.Width = 60

	return model{
		input:   ti,
		history: []string{},
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit

		case tea.KeyEnter:
			val := strings.TrimSpace(m.input.Value())
			if val != "" {
				m.history = append(m.history, val)
			}
			m.input.SetValue("")
		}
	}

	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) View() string {
	var sb strings.Builder

	sb.WriteString("╔══════════════════════════════════════════════╗\n")
	sb.WriteString("║              Bubbletea Echo App              ║\n")
	sb.WriteString("╚══════════════════════════════════════════════╝\n\n")

	if len(m.history) == 0 {
		sb.WriteString("  (nothing echoed yet)\n\n")
	} else {
		for i, line := range m.history {
			sb.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, line))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(m.input.View())
	sb.WriteString("\n\n  Press Esc or Ctrl+C to quit\n")

	return sb.String()
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
