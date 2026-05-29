// Package tui renders the interactive interface. It depends only on
// service.Service and performs no IO of its own: all loading and mutation is
// dispatched as tea.Cmd values that call the service off the UI goroutine.
package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	// agent is imported only to build the ssh-add command handed to
	// tea.ExecProcess, which yields the terminal so a passphrase prompt works.
	// All other IO goes through service.Service.
	"github.com/s-johri/sshush/pkg/agent"
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

// agentDoneMsg reports the outcome of an ssh-add add/remove run via ExecProcess.
type agentDoneMsg struct {
	verb string // "loaded" or "unloaded", for the status line
	err  error
}

// editDoneMsg reports the outcome of a host write (edit or delete).
type editDoneMsg struct {
	verb string // "saved" or "removed", for the status line
	err  error
}

// editMode tracks the host-editing overlay state.
type editMode int

const (
	modeNormal        editMode = iota
	modeNewKey                 // typing a new option name
	modeEdit                   // typing a value
	modeConfirm                // confirming a write
	modeConfirmDelete          // confirming a directive removal
)

// coreFields are always offered for editing; a host's existing option keys are
// appended to these when the edit overlay opens.
var coreFields = []string{"HostName", "User", "Port"}

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
	status  string // transient feedback (e.g. last agent action)

	// host edit overlay
	mode        editMode
	input       textinput.Model
	editFields  []string // core fields + the host's existing option keys
	fieldIdx    int      // index into editFields
	newKey      string   // option name being added (modeNewKey/modeEdit)
	pendingHost config.HostID

	width, height int
}

// New builds a Model bound to svc.
func New(svc service.Service) Model {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 40
	return Model{svc: svc, loading: true, input: ti}
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

	case agentDoneMsg:
		if msg.err != nil {
			m.status = "agent error: " + msg.err.Error()
			return m, nil
		}
		m.status = "key " + msg.verb
		// Re-sync so the loaded badge reflects the new agent state.
		m.loading = true
		return m, m.refresh

	case editDoneMsg:
		if msg.err != nil {
			m.status = "edit error: " + msg.err.Error()
			return m, nil
		}
		m.status = "host " + msg.verb + " (backup written)"
		m.loading = true
		return m, m.refresh

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Overlay modes capture input first.
	switch m.mode {
	case modeNewKey:
		return m.handleNewKey(msg)
	case modeEdit:
		return m.handleEditKey(msg)
	case modeConfirm, modeConfirmDelete:
		return m.handleConfirmKey(msg)
	}

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
	case "enter", " ":
		return m.toggleSelectedKey()
	case "e":
		return m.beginEdit()
	case "r":
		m.loading = true
		return m, m.refresh
	}
	return m, nil
}

// beginEdit opens the edit overlay on the selected host. Only directives the
// host actually has are listed; absent ones (e.g. Port) are added via ctrl+o.
func (m Model) beginEdit() (tea.Model, tea.Cmd) {
	host, ok := m.editTarget()
	if !ok {
		return m, nil
	}
	m.editFields = presentFields(host)
	m.fieldIdx = 0
	m.newKey = ""
	m.mode = modeEdit
	m.status = ""
	m.input.SetValue(m.currentFieldValue())
	m.input.CursorEnd()
	m.input.Focus()
	return m, textinput.Blink
}

// presentFields lists the directives set on a host, core fields first.
func presentFields(host config.Host) []string {
	var f []string
	if host.Hostname != "" {
		f = append(f, "HostName")
	}
	if host.User != "" {
		f = append(f, "User")
	}
	if host.Port != 0 {
		f = append(f, "Port")
	}
	opts := make([]string, 0, len(host.Options))
	for opt := range host.Options {
		opts = append(opts, opt)
	}
	sort.Strings(opts)
	return append(f, opts...)
}

// beginAddOption switches the open edit overlay to typing a new option name.
func (m Model) beginAddOption() (tea.Model, tea.Cmd) {
	m.mode = modeNewKey
	m.status = ""
	m.input.SetValue("")
	m.input.Focus()
	return m, textinput.Blink
}

// editTarget validates that a host is selected on the Hosts pane and records it.
func (m *Model) editTarget() (config.Host, bool) {
	if m.active != paneHosts {
		return config.Host{}, false
	}
	host, ok := m.selectedHost()
	if !ok {
		m.status = "no host selected"
		return config.Host{}, false
	}
	m.pendingHost = host.ID
	return host, true
}

// handleNewKey collects an option name, then transitions to value entry.
func (m Model) handleNewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.cancelOverlay("add cancelled")
	case "enter":
		key := strings.TrimSpace(m.input.Value())
		if key == "" {
			m.status = "option name cannot be empty"
			return m, nil
		}
		m.newKey = key
		m.mode = modeEdit
		m.input.SetValue("")
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleEditKey drives value entry. tab cycles fields (existing edits only);
// ctrl+d deletes the active directive; enter confirms.
func (m Model) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	editingExisting := m.newKey == "" && len(m.editFields) > 0
	switch msg.String() {
	case "esc":
		return m.cancelOverlay("edit cancelled")
	case "ctrl+o": // add a new option (works whether or not fields exist)
		if m.newKey == "" {
			return m.beginAddOption()
		}
	case "tab":
		if editingExisting { // cycling only applies to existing fields
			m.fieldIdx = (m.fieldIdx + 1) % len(m.editFields)
			m.input.SetValue(m.currentFieldValue())
			m.input.CursorEnd()
		}
		return m, nil
	case "ctrl+d":
		if editingExisting {
			m.mode = modeConfirmDelete
		}
		return m, nil
	case "enter":
		if m.newKey == "" && len(m.editFields) == 0 {
			m.status = "no directives set — ctrl+o to add one"
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) == "" {
			m.status = "value cannot be empty (ctrl+d to delete a directive)"
			return m, nil
		}
		m.mode = modeConfirm
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleConfirmKey gates writes and deletes: only "y" proceeds.
func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	confirming := m.mode
	if msg.String() != "y" && msg.String() != "Y" {
		return m.cancelOverlay("cancelled")
	}
	host := m.pendingHost
	field := m.activeField()
	m.input.Blur()
	m.mode = modeNormal
	m.status = "writing…"
	if confirming == modeConfirmDelete {
		return m, m.deleteCmd(host, field)
	}
	return m, m.editCmd(host, field, strings.TrimSpace(m.input.Value()))
}

func (m Model) cancelOverlay(status string) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	m.input.Blur()
	m.newKey = ""
	m.status = status
	return m, nil
}

// editCmd / deleteCmd dispatch the write off the UI goroutine.
func (m Model) editCmd(h config.HostID, field, val string) tea.Cmd {
	return func() tea.Msg { return editDoneMsg{verb: "saved", err: m.svc.EditHost(h, field, val)} }
}

func (m Model) deleteCmd(h config.HostID, field string) tea.Cmd {
	return func() tea.Msg { return editDoneMsg{verb: "removed", err: m.svc.DeleteHostField(h, field)} }
}

func (m Model) selectedHost() (config.Host, bool) {
	if len(m.hosts) == 0 {
		return config.Host{}, false
	}
	return m.hosts[m.cursor[paneHosts]], true
}

// activeField is the directive being edited: a newly typed option name, else
// the currently selected existing field.
func (m Model) activeField() string {
	if m.newKey != "" {
		return m.newKey
	}
	if m.fieldIdx < len(m.editFields) {
		return m.editFields[m.fieldIdx]
	}
	return ""
}

// currentFieldValue returns the selected host's value for the active field.
func (m Model) currentFieldValue() string {
	h, ok := m.selectedHost()
	if !ok {
		return ""
	}
	switch m.activeField() {
	case "HostName":
		return h.Hostname
	case "User":
		return h.User
	case "Port":
		if h.Port == 0 {
			return ""
		}
		return strconv.Itoa(h.Port)
	default:
		return h.Options[m.activeField()]
	}
}

// toggleSelectedKey loads or unloads the highlighted key in the agent via
// ssh-add, run through tea.ExecProcess so the terminal is free for a passphrase
// prompt. Only applies on the Keys pane to on-disk keys.
func (m Model) toggleSelectedKey() (tea.Model, tea.Cmd) {
	if m.active != paneKeys || len(m.ids) == 0 {
		return m, nil
	}
	sel := m.ids[m.cursor[paneKeys]]
	if !sel.ExistsOnDisk || sel.Path == "" {
		m.status = "cannot toggle agent-only key (no file on disk)"
		return m, nil
	}

	verb, cmd := "loaded", agent.AddCommand(sel.Path)
	if sel.LoadedInAgent {
		verb, cmd = "unloaded", agent.RemoveCommand(sel.Path)
	}
	m.status = "running ssh-add…"
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return agentDoneMsg{verb: verb, err: err}
	})
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

// View renders the current pane or an active overlay.
func (m Model) View() string {
	switch m.mode {
	case modeNewKey:
		return m.viewNewKey()
	case modeEdit:
		return m.viewEdit()
	case modeConfirm, modeConfirmDelete:
		return m.viewConfirm()
	}

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
	if m.status != "" {
		b.WriteString("  " + m.status + "\n")
	}
	b.WriteString(dimStyle.Render("  " + m.helpLine()))
	if m.srcFile != "" {
		b.WriteString(dimStyle.Render("   " + m.srcFile))
	}
	b.WriteString("\n")
	return b.String()
}

func (m Model) viewNewKey() string {
	var b strings.Builder
	b.WriteString(tabActive.Render("Add option to "+string(m.pendingHost)) + "\n\n")
	b.WriteString("  option name (e.g. ForwardAgent)\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(dimStyle.Render("  enter next · esc cancel"))
	b.WriteString("\n")
	return b.String()
}

func (m Model) viewEdit() string {
	var b strings.Builder
	title := "Edit host: " + string(m.pendingHost)
	if m.newKey != "" {
		title = "Add " + m.newKey + " to " + string(m.pendingHost)
	}
	b.WriteString(tabActive.Render(title) + "\n\n")
	switch {
	case m.newKey != "":
		b.WriteString("  " + m.newKey + "\n")
		b.WriteString("  " + m.input.View() + "\n\n")
		b.WriteString(dimStyle.Render("  enter confirm · esc cancel"))
	case len(m.editFields) == 0:
		b.WriteString(dimStyle.Render("  (no directives set)") + "\n\n")
		b.WriteString(dimStyle.Render("  ctrl+o add option · esc cancel"))
	default:
		b.WriteString("  " + m.activeField() + "\n")
		b.WriteString("  " + m.input.View() + "\n\n")
		b.WriteString(dimStyle.Render("  tab next · ctrl+o add option · ctrl+d delete · enter confirm · esc cancel"))
	}
	b.WriteString("\n")
	return b.String()
}

func (m Model) viewConfirm() string {
	field := m.activeField()
	var b strings.Builder
	if m.mode == modeConfirmDelete {
		b.WriteString(errStyle.Render("Confirm delete") + "\n\n")
		b.WriteString(fmt.Sprintf("  Remove %s from %s\n", field, m.pendingHost))
	} else {
		val := strings.TrimSpace(m.input.Value())
		b.WriteString(tabActive.Render("Confirm write") + "\n\n")
		b.WriteString(fmt.Sprintf("  Set %s of %s to %q\n", field, m.pendingHost, val))
	}
	b.WriteString(dimStyle.Render("  (a .bak backup of the config file is written first)") + "\n\n")
	b.WriteString("  " + rowActive.Render("y") + " write    " + rowActive.Render("n") + " cancel")
	b.WriteString("\n")
	return b.String()
}

// helpLine is the footer hint, tailored to the active pane.
func (m Model) helpLine() string {
	common := "↑/↓ move · tab switch · r refresh · q quit"
	if m.active == paneKeys {
		return "enter load/unload · " + common
	}
	return "e edit host · " + common
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
