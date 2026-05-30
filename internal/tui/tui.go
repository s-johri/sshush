// Package tui renders the interactive interface. It depends only on
// service.Service and performs no IO of its own: all loading and mutation is
// dispatched as tea.Cmd values that call the service off the UI goroutine.
package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	// agent is imported only to build the ssh-add command handed to
	// tea.ExecProcess, which yields the terminal so a passphrase prompt works.
	// All other IO goes through service.Service.
	"github.com/s-johri/sshush/pkg/agent"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
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

// editDoneMsg reports the outcome of a service call that mutates config/keys.
type editDoneMsg struct {
	verb string // status verb: "saved", "removed", "added", "deleted"
	err  error
}

// keygenDoneMsg reports the outcome of an interactive ssh-keygen run.
type keygenDoneMsg struct{ err error }

// tickMsg drives the periodic check that expires the status line.
type tickMsg struct{}

// statusTTL is how long a transient status line stays before auto-clearing.
const statusTTL = 4 * time.Second

// tick schedules the next status-expiry check.
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// editMode tracks the host-editing overlay state.
type editMode int

const (
	modeNormal         editMode = iota
	modeNewKey                  // typing a new option name (within edit)
	modeEdit                    // typing a value
	modeConfirm                 // confirming a write
	modeConfirmDelete           // confirming a directive removal
	modeNewHost                 // typing a new host alias / basic field
	modeNewHostOptKey           // new-host wizard: typing a custom option name
	modeNewHostOptVal           // new-host wizard: typing a custom option value
	modeNewKeyGen               // typing a new key file name
	modeConfirmDelHost          // confirming whole-host removal
	modeConfirmDelKey           // confirming key-file deletion (irreversible)
)

// coreFields are always offered for editing; a host's existing option keys are
// appended to these when the edit overlay opens.
var coreFields = []string{"HostName", "User", "Port"}

// hostSteps drives the new-host wizard: an alias (required) then optional basic
// fields. Empty answers are skipped.
var hostSteps = []struct{ field, hint string }{
	{"alias", "host alias (e.g. prod-web) — required"},
	{"HostName", "hostname / IP (optional, enter to skip)"},
	{"User", "user (optional, enter to skip)"},
	{"Port", "port (optional number, enter to skip)"},
}

// Model is the BubbleTea model for sshush.
type Model struct {
	svc service.Service

	active  pane
	cursor  [numPanes]int
	ids     []config.Identity // sorted for stable display
	hosts   []config.Host     // sorted for stable display
	srcFile string

	loading     bool
	err         error
	status      string    // transient feedback (e.g. last agent action)
	statusSetAt time.Time // when status last changed; status expires after statusTTL

	// host edit overlay
	mode        editMode
	input       textinput.Model
	editFields  []string // core fields + the host's existing option keys
	fieldIdx    int      // index into editFields
	newKey      string   // option name being added (modeNewKey/modeEdit)
	pendingHost config.HostID
	pendingKey  config.IdentityID // key targeted for deletion

	// new-host wizard
	hostStep    int
	draftHost   config.Host
	draftOptKey string // custom option name awaiting its value

	width, height int
}

// New builds a Model bound to svc.
func New(svc service.Service) Model {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 40
	return Model{svc: svc, loading: true, input: ti}
}

// Init kicks off the first refresh and starts the status-expiry ticker.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refresh, tick())
}

// refresh is a tea.Cmd: it loads a fresh snapshot off the UI goroutine.
func (m Model) refresh() tea.Msg {
	model, err := m.svc.Refresh()
	return refreshedMsg{model: model, err: err}
}

// Update wraps the core handler to timestamp status changes, so the ticker can
// expire the status line without nesting commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prev := m.status
	next, cmd := m.update(msg)
	mm := next.(Model)
	if mm.status != prev {
		mm.statusSetAt = time.Now()
	}
	return mm, cmd
}

// update is the core message handler.
func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		if m.status != "" && time.Since(m.statusSetAt) >= statusTTL {
			m.status = ""
		}
		return m, tick()

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
		m.status = msg.verb
		// Re-sync so the loaded badge reflects the new agent state.
		m.loading = true
		return m, m.refresh

	case editDoneMsg:
		if msg.err != nil {
			m.status = "error: " + msg.err.Error()
			return m, nil
		}
		m.status = msg.verb
		m.loading = true
		return m, m.refresh

	case keygenDoneMsg:
		if msg.err != nil {
			m.status = "ssh-keygen error: " + msg.err.Error()
			return m, nil
		}
		m.status = "key generated"
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
	case modeNewHost:
		return m.handleNewHost(msg)
	case modeNewHostOptKey:
		return m.handleNewHostOptKey(msg)
	case modeNewHostOptVal:
		return m.handleNewHostOptVal(msg)
	case modeNewKeyGen:
		return m.handleNewKeyGen(msg)
	case modeConfirmDelHost, modeConfirmDelKey:
		return m.handleDeleteConfirm(msg)
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
	case "U":
		return m.unloadAll()
	case "e":
		return m.beginEdit()
	case "n":
		return m.beginNew()
	case "d":
		return m.beginDelete()
	case "r":
		m.loading = true
		return m, m.refresh
	}
	return m, nil
}

// beginNew opens a creation flow: a new host on the Hosts pane, a new key on
// the Keys pane.
func (m Model) beginNew() (tea.Model, tea.Cmd) {
	m.status = ""
	m.input.SetValue("")
	m.input.Focus()
	if m.active == paneHosts {
		m.mode = modeNewHost
		m.hostStep = 0
		m.draftHost = config.Host{}
	} else {
		m.mode = modeNewKeyGen
	}
	return m, textinput.Blink
}

// beginDelete opens a confirm gate to delete the selected host or key.
func (m Model) beginDelete() (tea.Model, tea.Cmd) {
	if m.active == paneHosts {
		host, ok := m.selectedHost()
		if !ok {
			m.status = "no host to delete"
			return m, nil
		}
		m.pendingHost = host.ID
		m.mode = modeConfirmDelHost
		return m, nil
	}
	// Keys pane: only on-disk keys have files to delete.
	if len(m.ids) == 0 {
		return m, nil
	}
	sel := m.ids[m.cursor[paneKeys]]
	if !sel.ExistsOnDisk || sel.Path == "" {
		m.status = "cannot delete agent-only key (no file on disk)"
		return m, nil
	}
	m.pendingKey = sel.ID
	m.mode = modeConfirmDelKey
	return m, nil
}

// handleNewHost walks the new-host wizard: alias then optional basic fields,
// skipping empties. The final step dispatches AddHost.
func (m Model) handleNewHost(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		return m.cancelOverlay("cancelled")
	}
	if msg.String() != "enter" {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	val := strings.TrimSpace(m.input.Value())
	switch hostSteps[m.hostStep].field {
	case "alias":
		if val == "" {
			m.status = "host alias cannot be empty"
			return m, nil
		}
		m.draftHost.ID = config.HostID(val)
		m.draftHost.Name = val
	case "HostName":
		if val != "" {
			m.draftHost.Hostname = val
		}
	case "User":
		if val != "" {
			m.draftHost.User = val
		}
	case "Port":
		if val != "" {
			p, err := strconv.Atoi(val)
			if err != nil {
				m.status = "port must be a number"
				return m, nil
			}
			m.draftHost.Port = p
		}
	}

	if m.hostStep == len(hostSteps)-1 {
		// Basic fields done; move on to optional custom options.
		m.mode = modeNewHostOptKey
		m.input.SetValue("")
		return m, nil
	}
	m.hostStep++
	m.input.SetValue("")
	return m, nil
}

// handleNewHostOptKey collects a custom option name; a blank name finishes the
// wizard and creates the host.
func (m Model) handleNewHostOptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.cancelOverlay("cancelled")
	case "enter":
		key := strings.TrimSpace(m.input.Value())
		if key == "" {
			return m.createDraftHost()
		}
		m.draftOptKey = key
		m.mode = modeNewHostOptVal
		m.input.SetValue("")
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleNewHostOptVal stores a custom option value, then loops back for more.
func (m Model) handleNewHostOptVal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.cancelOverlay("cancelled")
	case "enter":
		val := strings.TrimSpace(m.input.Value())
		if val == "" {
			m.status = "value cannot be empty"
			return m, nil
		}
		if m.draftHost.Options == nil {
			m.draftHost.Options = map[string]string{}
		}
		m.draftHost.Options[m.draftOptKey] = val
		m.status = m.draftOptKey + " added"
		m.draftOptKey = ""
		m.mode = modeNewHostOptKey
		m.input.SetValue("")
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// createDraftHost dispatches AddHost for the accumulated draft.
func (m Model) createDraftHost() (tea.Model, tea.Cmd) {
	host := m.draftHost
	m.mode = modeNormal
	m.input.Blur()
	m.status = "creating host…"
	return m, func() tea.Msg { return editDoneMsg{verb: "host added", err: m.svc.AddHost(host)} }
}

// handleNewKeyGen collects a key file name and runs ssh-keygen interactively
// (via ExecProcess) so it can prompt for a passphrase.
func (m Model) handleNewKeyGen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.cancelOverlay("cancelled")
	case "enter":
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			m.status = "key name cannot be empty"
			return m, nil
		}
		m.mode = modeNormal
		m.input.Blur()
		m.status = "running ssh-keygen…"
		cmd, _, err := keys.GenerateCommand(keys.GenerateOpts{
			Name: name, Algorithm: config.AlgED25519, Comment: name,
		})
		if err != nil {
			m.status = "keygen error: " + err.Error()
			return m, nil
		}
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return keygenDoneMsg{err: err} })
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleDeleteConfirm gates host/key deletion: only "y" proceeds.
func (m Model) handleDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	mode := m.mode
	if msg.String() != "y" && msg.String() != "Y" {
		return m.cancelOverlay("deletion cancelled")
	}
	m.mode = modeNormal
	m.status = "deleting…"
	if mode == modeConfirmDelHost {
		h := m.pendingHost
		return m, func() tea.Msg { return editDoneMsg{verb: "host removed", err: m.svc.DeleteHost(h)} }
	}
	id := m.pendingKey
	return m, func() tea.Msg { return editDoneMsg{verb: "key deleted", err: m.svc.DeleteKey(id)} }
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

// unloadAll drops every key from the agent (Keys pane only).
func (m Model) unloadAll() (tea.Model, tea.Cmd) {
	if m.active != paneKeys {
		return m, nil
	}
	m.status = "unloading all keys…"
	return m, func() tea.Msg {
		return agentDoneMsg{verb: "all keys unloaded", err: m.svc.UnloadAllKeys()}
	}
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

	verb, cmd := "key loaded", agent.AddCommand(sel.Path)
	if sel.LoadedInAgent {
		verb, cmd = "key unloaded", agent.RemoveCommand(sel.Path)
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
	case modeNewHost:
		step := hostSteps[m.hostStep]
		title := fmt.Sprintf("New host (%d/%d)", m.hostStep+1, len(hostSteps))
		return m.viewPrompt(title, step.field+" — "+step.hint)
	case modeNewHostOptKey:
		return m.viewPrompt("New host: add option",
			"option name (e.g. ForwardAgent) — enter blank to finish")
	case modeNewHostOptVal:
		return m.viewPrompt("New host: add option", m.draftOptKey+" value")
	case modeNewKeyGen:
		return m.viewPrompt("Generate key", "file name (e.g. id_ed25519) — ed25519, may prompt passphrase")
	case modeConfirmDelHost:
		return m.viewDeleteConfirm(false)
	case modeConfirmDelKey:
		return m.viewDeleteConfirm(true)
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

// viewPrompt renders a single-line text prompt (new host / new key).
func (m Model) viewPrompt(title, hint string) string {
	var b strings.Builder
	b.WriteString(tabActive.Render(title) + "\n\n")
	b.WriteString("  " + hint + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(dimStyle.Render("  enter confirm · esc cancel"))
	b.WriteString("\n")
	return b.String()
}

// viewDeleteConfirm renders a y/n gate; key deletion is flagged irreversible.
func (m Model) viewDeleteConfirm(key bool) string {
	var b strings.Builder
	if key {
		b.WriteString(errStyle.Render("Delete key files — IRREVERSIBLE") + "\n\n")
		b.WriteString(fmt.Sprintf("  Permanently delete %s and its .pub from disk\n", m.pendingKey))
		b.WriteString(errStyle.Render("  the private key cannot be recovered") + "\n\n")
	} else {
		b.WriteString(errStyle.Render("Delete host") + "\n\n")
		b.WriteString(fmt.Sprintf("  Remove host %s from the config\n", m.pendingHost))
		b.WriteString(dimStyle.Render("  (a .bak backup of the config file is written first)") + "\n\n")
	}
	b.WriteString("  " + rowActive.Render("y") + " delete    " + rowActive.Render("n") + " cancel")
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
	common := "tab switch · r refresh · q quit"
	if m.active == paneKeys {
		return "enter load/unload · U unload all · n new key · d delete key · " + common
	}
	return "e edit · n new host · d delete host · " + common
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
