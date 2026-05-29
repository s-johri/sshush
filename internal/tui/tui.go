// Package tui renders the interactive interface. It depends only on
// service.Service and performs no IO of its own: all loading and mutation is
// dispatched as tea.Cmd values that call the service off the UI goroutine.
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/service"
)

type pane int

const (
	paneKeys pane = iota
	paneHosts
	numPanes
)

// refreshedMsg carries the result of a Service.Refresh dispatched as a command.
type refreshedMsg struct {
	model *config.SshConfigModel
	err   error
}

// Model is the BubbleTea model for sshush.
type Model struct {
	svc service.Service

	active  pane
	cursor  [numPanes]int
	ids     []config.Identity // sorted for stable display
	hosts   []config.Host     // sorted for stable display
	srcFile string

	loading bool
	err     error

	width, height int
}

// New builds a Model bound to svc.
func New(svc service.Service) Model {
	return Model{svc: svc, loading: true}
}

// Init kicks off the first refresh.
func (m Model) Init() tea.Cmd {
	return m.refresh
}

// refresh is a tea.Cmd: it loads a fresh snapshot off the UI goroutine.
func (m Model) refresh() tea.Msg {
	model, err := m.svc.Refresh()
	return refreshedMsg{model: model, err: err}
}

// Update handles messages and key input.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case refreshedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.applySnapshot(msg.model)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab", "left", "right", "h", "l":
		m.active = (m.active + 1) % numPanes
		return m, nil
	case "up", "k":
		if m.cursor[m.active] > 0 {
			m.cursor[m.active]--
		}
		return m, nil
	case "down", "j":
		if m.cursor[m.active] < m.rowCount()-1 {
			m.cursor[m.active]++
		}
		return m, nil
	case "r":
		m.loading = true
		return m, m.refresh
	}
	return m, nil
}

// applySnapshot turns a model into sorted display slices, clamping cursors.
func (m *Model) applySnapshot(snap *config.SshConfigModel) {
	m.ids = m.ids[:0]
	m.hosts = m.hosts[:0]
	if snap == nil {
		return
	}
	for _, id := range snap.Identities {
		m.ids = append(m.ids, id)
	}
	for _, h := range snap.Hosts {
		m.hosts = append(m.hosts, h)
	}
	sort.Slice(m.ids, func(i, j int) bool { return m.ids[i].Name < m.ids[j].Name })
	sort.Slice(m.hosts, func(i, j int) bool { return m.hosts[i].Name < m.hosts[j].Name })
	if len(snap.SourceFiles) > 0 {
		m.srcFile = snap.SourceFiles[0]
	}
	m.clampCursors()
}

func (m *Model) clampCursors() {
	for p := pane(0); p < numPanes; p++ {
		max := m.rowCountFor(p)
		if m.cursor[p] >= max {
			m.cursor[p] = max - 1
		}
		if m.cursor[p] < 0 {
			m.cursor[p] = 0
		}
	}
}

func (m Model) rowCount() int { return m.rowCountFor(m.active) }

func (m Model) rowCountFor(p pane) int {
	if p == paneKeys {
		return len(m.ids)
	}
	return len(m.hosts)
}

// --- styles (basic; polish pass is a later milestone) ---

var (
	tabActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	tabInactive = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	rowActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("159"))
	loadedBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// View renders the current pane.
func (m Model) View() string {
	var b strings.Builder

	keysTab, hostsTab := "Keys", "Hosts"
	if m.active == paneKeys {
		b.WriteString(tabActive.Render("[ "+keysTab+" ]") + " " + tabInactive.Render(hostsTab))
	} else {
		b.WriteString(tabInactive.Render(keysTab) + " " + tabActive.Render("[ "+hostsTab+" ]"))
	}
	b.WriteString("\n\n")

	switch {
	case m.loading:
		b.WriteString(dimStyle.Render("  loading…\n"))
	case m.err != nil:
		b.WriteString(errStyle.Render("  error: "+m.err.Error()) + "\n")
	case m.active == paneKeys:
		b.WriteString(m.viewKeys())
	default:
		b.WriteString(m.viewHosts())
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  ↑/↓ move · tab switch · r refresh · q quit"))
	if m.srcFile != "" {
		b.WriteString(dimStyle.Render("   " + m.srcFile))
	}
	b.WriteString("\n")
	return b.String()
}

func (m Model) viewKeys() string {
	if len(m.ids) == 0 {
		return dimStyle.Render("  no keys found\n")
	}
	var b strings.Builder
	for i, id := range m.ids {
		badge := dimStyle.Render("–")
		if id.LoadedInAgent {
			badge = loadedBadge.Render("✓")
		}
		algo := string(id.Algorithm)
		if !id.ExistsOnDisk {
			algo = "agent-only"
		}
		line := fmt.Sprintf("%s %-20s %-10s %s", badge, id.Name, algo, dimStyle.Render(id.Comment))
		b.WriteString(m.renderRow(paneKeys, i, line))
	}
	return b.String()
}

func (m Model) viewHosts() string {
	if len(m.hosts) == 0 {
		return dimStyle.Render("  no hosts found\n")
	}
	var b strings.Builder
	for i, h := range m.hosts {
		dest := h.Hostname
		if h.User != "" {
			dest = h.User + "@" + dest
		}
		if h.Port != 0 {
			dest = fmt.Sprintf("%s:%d", dest, h.Port)
		}
		line := fmt.Sprintf("%-20s %s", h.Name, dimStyle.Render(dest))
		b.WriteString(m.renderRow(paneHosts, i, line))
	}
	return b.String()
}

// renderRow draws one list row, marking the active pane's cursor row.
func (m Model) renderRow(p pane, i int, content string) string {
	cursor := "  "
	if p == m.active && i == m.cursor[p] {
		return rowActive.Render("▸ "+content) + "\n"
	}
	return cursor + content + "\n"
}
