// Package tui renders the interactive interface. It depends only on
// service.Service and performs no IO of its own: all loading and mutation is
// dispatched as tea.Cmd values that call the service off the UI goroutine.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
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
	"github.com/s-johri/sshush/pkg/watch"
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

// fileWatcher reports coalesced filesystem changes under directories it is told
// to watch. The real implementation is pkg/watch; the TUI depends on this
// narrow interface so it can run without a watcher (and be tested with a fake).
type fileWatcher interface {
	Watch(dirs []string) error
	Events() <-chan struct{}
}

// appSettings persists sshush's own preferences (default identity). The TUI
// depends on this narrow interface so it can run without settings and be tested
// with a fake. The real implementation is pkg/appconfig.Store.
type appSettings interface {
	DefaultIdentity() config.IdentityID
	AutoLoad() bool
	SetDefaultIdentity(config.IdentityID) error
	ClearDefault() error
}

// reloadMsg signals that watched files changed and the model should refresh.
type reloadMsg struct{}

// reloadMuteWindow is how long after a write made *by this app* to ignore the
// resulting filesystem events, so self-induced changes don't show a spurious
// "files changed" reload (the mutation already refreshed). The event arrives
// ~one debounce after our write, so this is the debounce plus a small margin —
// kept short so genuine external changes are missed for as little as possible.
const reloadMuteWindow = watch.Debounce + 500*time.Millisecond

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
	modeKeyPicker               // attaching/detaching keys to a host
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

	// key picker (attach/detach identities to pendingHost)
	pickerCursor int

	// hot reload
	watcher         fileWatcher
	pendingReload   bool      // a change arrived while an overlay was open
	muteReloadUntil time.Time // ignore reloads until this time (self-induced writes)

	// app settings / default identity
	settings   appSettings
	autoLoaded bool // startup auto-load of the default identity has run

	width, height int
}

// New builds a Model bound to svc.
func New(svc service.Service) Model {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 40
	return Model{svc: svc, loading: true, input: ti}
}

// WithWatcher attaches a filesystem watcher for hot reload. Optional: without
// one, the model still works and refreshes only on explicit actions.
func (m Model) WithWatcher(w fileWatcher) Model {
	m.watcher = w
	return m
}

// WithSettings attaches persisted app settings (default identity). Optional.
func (m Model) WithSettings(s appSettings) Model {
	m.settings = s
	return m
}

// Init kicks off the first refresh, the status ticker, and (if present) the
// file-watch listener.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.refresh, tick()}
	if c := m.waitForChange(); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

// waitForChange blocks on the watcher until a change arrives, then yields a
// reloadMsg. Returns nil when no watcher is attached.
func (m Model) waitForChange() tea.Cmd {
	w := m.watcher
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		<-w.Events()
		return reloadMsg{}
	}
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
		// Apply a deferred reload once any overlay is closed.
		if m.pendingReload && m.mode == modeNormal && !m.loading {
			m.pendingReload = false
			m.loading = true
			return m, tea.Batch(tick(), m.refresh)
		}
		return m, tick()

	case reloadMsg:
		// Re-arm the listener regardless of what we do with this event.
		next := m.waitForChange()
		// Ignore events caused by our own recent writes.
		if time.Now().Before(m.muteReloadUntil) {
			return m, next
		}
		// Only refresh when idle so an open overlay or in-flight load is never
		// clobbered.
		if m.mode != modeNormal || m.loading {
			m.pendingReload = true
			return m, next
		}
		m.loading = true
		m.status = "reloaded (files changed)"
		return m, tea.Batch(next, m.refresh)

	case refreshedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.applySnapshot(msg.model)
		return m.maybeAutoLoad()

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
		m.muteReloadUntil = time.Now().Add(reloadMuteWindow) // our write, not external
		m.loading = true
		return m, m.refresh

	case keygenDoneMsg:
		if msg.err != nil {
			m.status = "ssh-keygen error: " + msg.err.Error()
			return m, nil
		}
		m.status = "key generated"
		m.muteReloadUntil = time.Now().Add(reloadMuteWindow)
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
	case modeKeyPicker:
		return m.handlePicker(msg)
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
	case "s":
		return m.setDefaultKey()
	case "e":
		return m.beginEdit()
	case "i":
		return m.beginKeyPicker()
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

// maybeAutoLoad loads the configured default identity into the agent once, on
// the first snapshot, if it exists on disk and is not already loaded.
func (m Model) maybeAutoLoad() (tea.Model, tea.Cmd) {
	if m.autoLoaded || m.settings == nil || !m.settings.AutoLoad() {
		return m, nil
	}
	m.autoLoaded = true // attempt only once per session

	id := m.settings.DefaultIdentity()
	if id == "" {
		return m, nil
	}
	for _, ident := range m.ids {
		if ident.ID == id && ident.ExistsOnDisk && !ident.LoadedInAgent {
			m.status = "loading default key…"
			return m, tea.ExecProcess(agent.AddCommand(ident.Path),
				func(err error) tea.Msg { return agentDoneMsg{verb: "default key loaded", err: err} })
		}
	}
	return m, nil
}

// setDefaultKey toggles the selected key as the startup default (Keys pane):
// setting it, or unsetting it when it is already the default.
func (m Model) setDefaultKey() (tea.Model, tea.Cmd) {
	if m.active != paneKeys || m.settings == nil || len(m.ids) == 0 {
		if m.settings == nil {
			m.status = "no settings file configured"
		}
		return m, nil
	}
	sel := m.ids[m.cursor[paneKeys]]

	if sel.ID == m.settings.DefaultIdentity() {
		if err := m.settings.ClearDefault(); err != nil {
			m.status = "settings error: " + err.Error()
			return m, nil
		}
		m.status = "default key unset"
		return m, nil
	}
	if !sel.ExistsOnDisk {
		m.status = "cannot set agent-only key as default"
		return m, nil
	}
	if err := m.settings.SetDefaultIdentity(sel.ID); err != nil {
		m.status = "settings error: " + err.Error()
		return m, nil
	}
	m.status = "default key set: " + sel.Name
	return m, nil
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

// hostByID returns the current model copy of a host (post-refresh).
func (m Model) hostByID(id config.HostID) (config.Host, bool) {
	for _, h := range m.hosts {
		if h.ID == id {
			return h, true
		}
	}
	return config.Host{}, false
}

// diskKeys returns identities that have a key file on disk (attachable to hosts).
func (m Model) diskKeys() []config.Identity {
	var out []config.Identity
	for _, id := range m.ids {
		if id.ExistsOnDisk {
			out = append(out, id)
		}
	}
	return out
}

func hostHasIdentity(h config.Host, id config.IdentityID) bool {
	for _, x := range h.Identities {
		if x == id {
			return true
		}
	}
	return false
}

// beginKeyPicker opens the attach/detach overlay for the selected host.
func (m Model) beginKeyPicker() (tea.Model, tea.Cmd) {
	host, ok := m.selectedHost()
	if !ok {
		m.status = "no host selected"
		return m, nil
	}
	if len(m.diskKeys()) == 0 {
		m.status = "no on-disk keys to associate"
		return m, nil
	}
	m.pendingHost = host.ID
	m.pickerCursor = 0
	m.mode = modeKeyPicker
	m.status = ""
	return m, nil
}

// handlePicker drives the attach/detach overlay. enter toggles association of
// the highlighted key with the host; the overlay stays open for more changes.
func (m Model) handlePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	disk := m.diskKeys()
	switch msg.String() {
	case "esc", "q":
		m.mode = modeNormal
		return m, nil
	case "up", "k":
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}
		return m, nil
	case "down", "j":
		if m.pickerCursor < len(disk)-1 {
			m.pickerCursor++
		}
		return m, nil
	case "enter", " ":
		if m.pickerCursor >= len(disk) {
			return m, nil
		}
		host, ok := m.hostByID(m.pendingHost)
		if !ok {
			m.mode = modeNormal
			return m, nil
		}
		sel := disk[m.pickerCursor]
		h := m.pendingHost
		if hostHasIdentity(host, sel.ID) {
			return m, func() tea.Msg { return editDoneMsg{verb: "key detached", err: m.svc.DetachKey(h, sel.ID)} }
		}
		return m, func() tea.Msg { return editDoneMsg{verb: "key attached", err: m.svc.AttachKey(h, sel.ID)} }
	}
	return m, nil
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
	m.rewatch(snap.SourceFiles)
	m.clampCursors()
}

// rewatch points the file watcher at the directories of the config source files
// plus ~/.ssh (key changes), so external edits trigger a reload. Best-effort.
func (m *Model) rewatch(sourceFiles []string) {
	if m.watcher == nil {
		return
	}
	seen := map[string]bool{}
	var dirs []string
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	for _, f := range sourceFiles {
		add(filepath.Dir(f))
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".ssh"))
	}
	_ = m.watcher.Watch(dirs)
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
	colPrimary = lipgloss.Color("212") // pink — accents, active tab
	colAccent  = lipgloss.Color("159") // light cyan — selection text
	colGreen   = lipgloss.Color("42")  // loaded badge
	colDim     = lipgloss.Color("244") // muted text
	colErr     = lipgloss.Color("203") // errors / destructive
	colGold    = lipgloss.Color("220") // default-key star
	colBorder  = lipgloss.Color("240") // box borders
	colSelBg   = lipgloss.Color("236") // selected-row background

	appTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colPrimary)
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(colPrimary)
	tabSelected   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(colPrimary).Padding(0, 1)
	tabUnselected = lipgloss.NewStyle().Foreground(colDim).Padding(0, 1)
	headerStyle   = lipgloss.NewStyle().Foreground(colDim).Underline(true)
	selectedRow   = lipgloss.NewStyle().Bold(true).Foreground(colAccent).Background(colSelBg)
	loadedBadge   = lipgloss.NewStyle().Foreground(colGreen)
	dimStyle      = lipgloss.NewStyle().Foreground(colDim)
	errStyle      = lipgloss.NewStyle().Bold(true).Foreground(colErr)
	starStyle     = lipgloss.NewStyle().Foreground(colGold)
	keyCap        = lipgloss.NewStyle().Bold(true).Foreground(colGold)
	statusStyle   = lipgloss.NewStyle().Foreground(colAccent)
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colBorder).Padding(0, 1)
	helpKey       = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	helpLabel     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	hostTagStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("109")) // muted teal — hosts using a key
)

// tabActive is kept as the overlay/title accent style for readability.
var tabActive = titleStyle

// View renders the current pane or an active overlay.
func (m Model) View() string {
	switch m.mode {
	case modeNewKey:
		return m.card(m.viewNewKey())
	case modeEdit:
		return m.card(m.viewEdit())
	case modeConfirm, modeConfirmDelete:
		return m.card(m.viewConfirm())
	case modeNewHost:
		step := hostSteps[m.hostStep]
		title := fmt.Sprintf("New host (%d/%d)", m.hostStep+1, len(hostSteps))
		return m.card(m.viewPrompt(title, step.field+" — "+step.hint))
	case modeNewHostOptKey:
		return m.card(m.viewPrompt("New host: add option",
			"option name (e.g. ForwardAgent) — enter blank to finish"))
	case modeNewHostOptVal:
		return m.card(m.viewPrompt("New host: add option", m.draftOptKey+" value"))
	case modeNewKeyGen:
		return m.card(m.viewPrompt("Generate key", "file name (e.g. id_ed25519) — ed25519, may prompt passphrase"))
	case modeConfirmDelHost:
		return m.card(m.viewDeleteConfirm(false))
	case modeConfirmDelKey:
		return m.card(m.viewDeleteConfirm(true))
	case modeKeyPicker:
		return m.card(m.viewPicker())
	}

	header := appTitleStyle.Render("sshush") + "   " + m.renderTabs()

	var body string
	switch {
	case m.loading:
		body = dimStyle.Render("loading…")
	case m.err != nil:
		body = errStyle.Render("error: " + m.err.Error())
	case m.active == paneKeys:
		body = m.viewKeys()
	default:
		body = m.viewHosts()
	}

	var f strings.Builder
	f.WriteString(header + "\n")
	f.WriteString(m.box(body) + "\n")
	if m.status != "" {
		f.WriteString(statusStyle.Render("  "+m.status) + "\n")
	}
	f.WriteString(m.renderHelp() + "\n")
	if m.srcFile != "" {
		f.WriteString(dimStyle.Render("  "+m.srcFile) + "\n")
	}
	return f.String()
}

// renderTabs draws the Keys/Hosts tab bar with the active tab highlighted.
func (m Model) renderTabs() string {
	keys, hosts := tabUnselected.Render("Keys"), tabUnselected.Render("Hosts")
	if m.active == paneKeys {
		keys = tabSelected.Render("Keys")
	} else {
		hosts = tabSelected.Render("Hosts")
	}
	return keys + " " + hosts
}

// box wraps pane content in a rounded border, sized to the terminal width when
// known.
func (m Model) box(content string) string {
	s := boxStyle
	if m.width > 4 {
		s = s.Width(m.width - 2)
	}
	return s.Render(strings.TrimRight(content, "\n"))
}

// card wraps an overlay in a rounded border, separated from the top edge.
func (m Model) card(content string) string {
	return "\n" + boxStyle.Render(strings.TrimRight(content, "\n")) + "\n"
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

// viewPicker renders the attach/detach overlay: disk keys with a ✓ for those
// associated with the host via IdentityFile.
func (m Model) viewPicker() string {
	host, _ := m.hostByID(m.pendingHost)
	disk := m.diskKeys()
	var b strings.Builder
	b.WriteString(tabActive.Render("Keys for host: "+string(m.pendingHost)) + "\n\n")
	for i, id := range disk {
		attached := hostHasIdentity(host, id.ID)
		glyph := glyphUnloaded
		glyphStyle := dimStyle
		if attached {
			glyph, glyphStyle = glyphLoaded, loadedBadge
		}
		if i == m.pickerCursor {
			b.WriteString(selectedRow.Render("▸ "+glyph+" "+id.Name) + "\n")
		} else {
			b.WriteString("  " + glyphStyle.Render(glyph) + " " + id.Name + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  ↑/↓ move · enter attach/detach · esc close"))
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
		if h, ok := m.hostByID(m.pendingHost); ok && h.IsPattern {
			b.WriteString(errStyle.Render("  this is a wildcard block — removes defaults for every matching connection") + "\n")
		}
		b.WriteString(dimStyle.Render("  (a .bak backup of the config file is written first)") + "\n\n")
	}
	b.WriteString("  " + keyCap.Render("y") + " delete    " + keyCap.Render("n") + " cancel")
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
	if h, ok := m.hostByID(m.pendingHost); ok && h.IsPattern {
		b.WriteString(errStyle.Render("  this is a wildcard block — affects every matching connection") + "\n")
	}
	b.WriteString(dimStyle.Render("  (a .bak backup of the config file is written first)") + "\n\n")
	b.WriteString("  " + keyCap.Render("y") + " write    " + keyCap.Render("n") + " cancel")
	b.WriteString("\n")
	return b.String()
}

// helpLine is the footer hint, tailored to the active pane.
type helpItem struct{ key, desc string }
type helpGroup struct {
	label string
	items []helpItem
}

// helpGroups returns the keybinding hints for the active pane, grouped into
// labeled categories for a less cluttered footer.
func (m Model) helpGroups() []helpGroup {
	view := helpGroup{"view", []helpItem{{"tab", "panes"}, {"r", "refresh"}, {"q", "quit"}}}
	if m.active == paneKeys {
		return []helpGroup{
			{"agent", []helpItem{{"↵", "load/unload"}, {"U", "unload all"}, {"s", "default"}}},
			{"keys", []helpItem{{"n", "new"}, {"d", "delete"}}},
			view,
		}
	}
	return []helpGroup{
		{"hosts", []helpItem{{"e", "edit"}, {"i", "keys"}, {"n", "new"}, {"d", "delete"}}},
		view,
	}
}

// renderHelp lays the grouped hints out one category per line: a padded label,
// then "key desc" pairs.
func (m Model) renderHelp() string {
	sep := dimStyle.Render(" · ")
	var lines []string
	for _, g := range m.helpGroups() {
		parts := make([]string, len(g.items))
		for i, it := range g.items {
			parts[i] = helpKey.Render(it.key) + " " + dimStyle.Render(it.desc)
		}
		label := helpLabel.Render(fmt.Sprintf("%-6s", g.label))
		lines = append(lines, "  "+label+strings.Join(parts, sep))
	}
	return strings.Join(lines, "\n")
}

// loadGlyph is a filled circle for keys present in the agent, a hollow one for
// keys that are not.
const (
	glyphLoaded   = "●"
	glyphUnloaded = "○"
)

func (m Model) viewKeys() string {
	if len(m.ids) == 0 {
		return dimStyle.Render("no keys found")
	}
	var defaultID config.IdentityID
	if m.settings != nil {
		defaultID = m.settings.DefaultIdentity()
	}
	usedBy := m.hostsByKey()
	lines := []string{headerStyle.Render(fmt.Sprintf("  %1s %-20s %-11s %s", " ", "name", "algo", "comment / hosts"))}
	for i, id := range m.ids {
		glyph := glyphUnloaded
		glyphStyle := dimStyle
		if id.LoadedInAgent {
			glyph, glyphStyle = glyphLoaded, loadedBadge
		}
		algo := string(id.Algorithm)
		if !id.ExistsOnDisk {
			algo = "agent-only"
		}
		nameCol := fmt.Sprintf("%-20s", id.Name)
		algoCol := fmt.Sprintf("%-11s", algo)

		// plain: one uncolored string, so the selected-row background fills it.
		plain := fmt.Sprintf("%s %s %s %s", glyph, nameCol, algoCol, id.Comment)
		// styled: per-segment colors for the non-selected rows.
		styled := glyphStyle.Render(glyph) + " " + nameCol + " " + algoCol + " " + dimStyle.Render(id.Comment)
		if hosts := usedBy[id.ID]; len(hosts) > 0 {
			tag := "↪ " + strings.Join(hosts, ", ")
			plain += "  " + tag
			styled += "  " + hostTagStyle.Render(tag)
		}
		if id.ID == defaultID {
			plain += "  ★ default"
			styled += "  " + starStyle.Render("★ default")
		}
		lines = append(lines, m.listRow(paneKeys, i, plain, styled))
	}
	return strings.Join(lines, "\n")
}

// hostsByKey maps each identity to the names of hosts that reference it via
// IdentityFile. Host order follows m.hosts (already sorted), keeping it stable.
func (m Model) hostsByKey() map[config.IdentityID][]string {
	used := map[config.IdentityID][]string{}
	for _, h := range m.hosts {
		for _, id := range h.Identities {
			used[id] = append(used[id], h.Name)
		}
	}
	return used
}

func (m Model) viewHosts() string {
	if len(m.hosts) == 0 {
		return dimStyle.Render("no hosts found")
	}
	lines := []string{headerStyle.Render(fmt.Sprintf("  %-20s %s", "host", "destination"))}
	for i, h := range m.hosts {
		dest := h.Hostname
		if h.User != "" {
			dest = h.User + "@" + dest
		}
		if h.Port != 0 {
			dest = fmt.Sprintf("%s:%d", dest, h.Port)
		}
		if h.IsPattern {
			dest = "pattern defaults"
		}
		if n := len(h.Identities); n > 0 {
			unit := "keys"
			if n == 1 {
				unit = "key"
			}
			dest = fmt.Sprintf("%s  [%d %s]", dest, n, unit)
		}
		nameCol := fmt.Sprintf("%-20s", h.Name)
		plain := nameCol + " " + dest
		styled := nameCol + " " + dimStyle.Render(dest)
		lines = append(lines, m.listRow(paneHosts, i, plain, styled))
	}
	return strings.Join(lines, "\n")
}

// listRow renders one list row (no trailing newline). The active pane's cursor
// row uses the plain text with a full-width highlight (embedding pre-styled
// spans would reset the background mid-row); other rows use the styled text.
func (m Model) listRow(p pane, i int, plain, styled string) string {
	if p == m.active && i == m.cursor[p] {
		s := selectedRow
		if m.width > 4 {
			s = s.Width(m.width - 4) // inner width: minus border + padding
		}
		return s.Render("▸ " + plain)
	}
	return "  " + styled
}
