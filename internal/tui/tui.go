// Package tui renders the interactive interface. It depends only on
// service.Service and performs no IO of its own: all loading and mutation is
// dispatched as tea.Cmd values that call the service off the UI goroutine.
package tui

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	// agent is imported only to build the ssh-add command handed to
	// tea.ExecProcess, which yields the terminal so a passphrase prompt works.
	// All other IO goes through service.Service.
	"github.com/s-johri/sshush/pkg/agent"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/service"
	"github.com/s-johri/sshush/pkg/shellinit"
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

// restoreDoneMsg reports the outcome of a restore-from-backup.
type restoreDoneMsg struct {
	files []string
	err   error
}

// updateCheckMsg carries the result of the async launch update-check.
type updateCheckMsg struct {
	tag       string // latest release tag, when newer
	available bool
}

// connectDoneMsg reports that an `ssh <alias>` session (run via ExecProcess)
// has ended.
type connectDoneMsg struct {
	alias string
	err   error
}

// copyOption is one entry in the clipboard copy menu.
type copyOption struct {
	key     string // hotkey to pick it
	label   string
	content string
}

// clipDoneMsg reports the outcome of a clipboard write.
type clipDoneMsg struct {
	label string
	err   error
}

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
	DefaultIdentities() []config.IdentityID
	IsDefault(config.IdentityID) bool
	AutoLoad() bool
	ToggleDefault(config.IdentityID) (bool, error)
	MotionEnabled() bool
	MotionIntensity() string
	SetMotion(enabled bool, intensity string) error
	ThemeName() string
	SetTheme(name string) error
}

// reloadMsg signals that watched files changed and the model should refresh.
type reloadMsg struct{}

// reloadMuteWindow is how long after a write made *by this app* to ignore the
// resulting filesystem events, so self-induced changes don't show a spurious
// "files changed" reload (the mutation already refreshed). The event arrives
// ~one debounce after our write, so this is the debounce plus a small margin —
// kept short so genuine external changes are missed for as little as possible.
const reloadMuteWindow = watch.Debounce + 500*time.Millisecond

// --- motion (opt-in animation system) ---

// activeFX is a time-bounded visual effect layered over the render. A flash
// (good/bad) and a screen-shake can run together, both driven by one normalized
// progress so the motion feels cohesive. Zero value = nothing playing.
type activeFX struct {
	flashGood bool
	flashBad  bool
	shakeAmp  int // peak horizontal shake in columns; 0 = no shake
	start     time.Time
	dur       time.Duration
}

func (f activeFX) any() bool {
	return f.flashGood || f.flashBad || f.shakeAmp > 0
}

// frameRate is fast (≈60fps) so the shake reads as snappy, not laggy.
const frameRate = 16 * time.Millisecond

// frameMsg drives animation frames; only scheduled while an effect is active so
// there is no idle CPU cost.
type frameMsg struct{}

func frameTick() tea.Cmd {
	return tea.Tick(frameRate, func(time.Time) tea.Msg { return frameMsg{} })
}

// motionOn reports whether the motion system is enabled.
func (m Model) motionOn() bool { return m.settings != nil && m.settings.MotionEnabled() }

// fxActive reports whether an effect is currently playing.
func (m Model) fxActive() bool { return m.fx.any() && time.Since(m.fx.start) < m.fx.dur }

// fxProgress is the effect's normalized elapsed time in [0,1].
func (m Model) fxProgress() float64 {
	p := float64(time.Since(m.fx.start)) / float64(m.fx.dur)
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

// fxParams returns the (duration, shake-amplitude) for the current intensity.
// Short and distinct so levels are obvious; the 16ms frame keeps it snappy.
func (m Model) fxParams() (time.Duration, int) {
	switch m.settings.MotionIntensity() {
	case "subtle":
		return 180 * time.Millisecond, 2
	case "arcade":
		return 420 * time.Millisecond, 7
	default: // normal
		return 260 * time.Millisecond, 4
	}
}

// play starts a flash (+ optional shake) if motion is on, returning the frame
// ticker. forceShake adds a shake even at non-arcade levels (destructive ops);
// arcade always shakes.
func (m *Model) play(good, forceShake bool) tea.Cmd {
	if !m.motionOn() {
		return nil
	}
	dur, amp := m.fxParams()
	shake := 0
	if forceShake || m.settings.MotionIntensity() == "arcade" || !good {
		shake = amp
	}
	m.fx = activeFX{flashGood: good, flashBad: !good, shakeAmp: shake, start: time.Now(), dur: dur}
	return frameTick()
}

// applyShake jitters the whole frame horizontally with a punchy envelope: the
// amplitude starts high and decays fast (ease-out²), oscillating at high
// frequency, so it hits hard then settles — not a slow linear wobble.
func (m Model) applyShake(s string) string {
	if m.fx.shakeAmp <= 0 {
		return s
	}
	decay := 1 - m.fxProgress()
	decay *= decay // (1-p)² — punchy ease-out
	osc := math.Abs(math.Sin(time.Since(m.fx.start).Seconds() * 38 * math.Pi))
	off := int(math.Round(float64(m.fx.shakeAmp) * decay * osc))
	if off == 0 {
		return s
	}
	pad := strings.Repeat(" ", off)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// tickMsg drives the periodic check that expires the status line.
type tickMsg struct{}

// statusTTL is how long a transient status line stays before auto-clearing.
const statusTTL = 4 * time.Second

// tick schedules the next status-expiry check.
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// keyAlgos are the algorithms offered by the new-key wizard (dsa omitted as
// legacy). ed25519 first as the recommended default.
var keyAlgos = []struct {
	algo  config.KeyAlgorithm
	label string
}{
	{config.AlgED25519, "ed25519 — recommended; modern, small, fast"},
	{config.AlgRSA, "rsa — widely compatible"},
	{config.AlgECDSA, "ecdsa — NIST curves"},
}

// bitsOptions returns the selectable -b values for an algorithm: key size for
// rsa, curve size for ecdsa, none for ed25519.
func bitsOptions(a config.KeyAlgorithm) []int {
	switch a {
	case config.AlgRSA:
		return []int{3072, 4096, 2048}
	case config.AlgECDSA:
		return []int{256, 384, 521}
	}
	return nil
}

// Model is the BubbleTea model for sshush.
type Model struct {
	svc service.Service

	active  pane
	vp      [numPanes]viewport // cursor + scroll per pane (see CONTEXT.md)
	ids     []config.Identity  // sorted for stable display
	hosts   []config.Host      // sorted for stable display
	srcFile string

	loading     bool
	err         error
	status      string    // transient feedback (e.g. last agent action)
	statusSetAt time.Time // when status last changed; status expires after statusTTL

	// modal is the active overlay, or nil for the panes (see overlay.go and
	// CONTEXT.md). Each overlay owns its own working state.
	modal overlay

	// motion: the currently-active visual effect (zero value = none)
	fx activeFX

	// hot reload
	watcher         fileWatcher
	pendingReload   bool      // a change arrived while an overlay was open
	muteReloadUntil time.Time // ignore reloads until this time (self-induced writes)

	// search / filter (per active pane)
	filterInput textinput.Model
	filtering   bool // true while the filter input is focused

	// app settings / default identity
	settings   appSettings
	autoLoaded bool   // startup auto-load of the default identity has run
	sshDir     string // configured SSH dir to watch (empty => ~/.ssh)

	// cfgFlag is the custom SSH config path to pass to ssh as -F. Empty means
	// the user runs on the default config — never pass -F then, because -F
	// also suppresses /etc/ssh/ssh_config.
	cfgFlag string

	// checkUpdates, when set, returns (latest tag, newer-available) for the
	// async launch update-check. nil disables it (dev build / opted out).
	checkUpdates func() (string, bool)

	width, height int
}

// New builds a Model bound to svc.
func New(svc service.Service) Model {
	fi := textinput.New()
	fi.Placeholder = "filter…"
	fi.CharLimit = 80
	fi.Width = 30

	return Model{svc: svc, loading: true, filterInput: fi}
}

// filterQuery is the active (lower-cased, trimmed) filter string.
func (m Model) filterQuery() string {
	return strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
}

// visibleIDs / visibleHosts are the rows shown in each pane: the full sorted
// list, or its filtered subset when a filter is active. All display, selection,
// and scrolling operate on these, not the raw m.ids/m.hosts.
func (m Model) visibleIDs() []config.Identity {
	q := m.filterQuery()
	if q == "" {
		return m.ids
	}
	var out []config.Identity
	for _, id := range m.ids {
		if matchAny(q, id.Name, id.Comment, string(id.Algorithm)) {
			out = append(out, id)
		}
	}
	return out
}

func (m Model) visibleHosts() []config.Host {
	q := m.filterQuery()
	if q == "" {
		return m.hosts
	}
	var out []config.Host
	for _, h := range m.hosts {
		if matchAny(q, h.Name, h.Hostname, h.User) {
			out = append(out, h)
		}
	}
	return out
}

// matchAny reports whether q (already lower-cased) is a substring of any field.
func matchAny(q string, fields ...string) bool {
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// selectedKey returns the highlighted identity in the (filtered) Keys pane.
func (m Model) selectedKey() (config.Identity, bool) {
	v := m.visibleIDs()
	i := m.vp[paneKeys].cursor
	if i < 0 || i >= len(v) {
		return config.Identity{}, false
	}
	return v[i], true
}

// handleFilterKey drives the live filter input. enter applies and exits the
// input (keeping the query); esc clears it; anything else edits and re-filters.
func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filterInput.SetValue("")
		m.filterInput.Blur()
		m.afterFilterChange()
		return m, nil
	case "enter":
		m.filtering = false
		m.filterInput.Blur()
		m.afterFilterChange()
		return m, nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.afterFilterChange()
	return m, cmd
}

// afterFilterChange keeps the cursor/scroll valid for the (possibly smaller)
// filtered list of the active pane.
func (m *Model) afterFilterChange() {
	m.vp[m.active].clampCursor(m.rowCountFor(m.active))
	m.vp[m.active].scroll = 0
	m.ensureVisible(m.active)
}

// WithWatcher attaches a filesystem watcher for hot reload. Optional: without
// one, the model still works and refreshes only on explicit actions.
func (m Model) WithWatcher(w fileWatcher) Model {
	m.watcher = w
	return m
}

// WithSettings attaches persisted app settings (default identity, theme, …) and
// applies the saved theme. Optional.
func (m Model) WithSettings(s appSettings) Model {
	m.settings = s
	if t, ok := themeByName(s.ThemeName()); ok {
		applyTheme(t)
	}
	return m
}

// WithSshDir sets the SSH directory the hot-reload watcher should observe (in
// addition to the config source dirs). Empty means ~/.ssh.
func (m Model) WithSshDir(dir string) Model {
	m.sshDir = dir
	return m
}

// WithConfigFlag makes connect/copy commands carry `-F path` so ssh resolves
// hosts against the custom config sshush is managing. Only set when the user
// configured a non-default config location.
func (m Model) WithConfigFlag(path string) Model {
	m.cfgFlag = path
	return m
}

// sshArgs builds the argv (after "ssh") for connecting to alias.
func (m Model) sshArgs(alias string) []string {
	if m.cfgFlag != "" {
		return []string{"-F", m.cfgFlag, alias}
	}
	return []string{alias}
}

// WithUpdateCheck enables the async launch update-check. check returns the
// latest release tag and whether it is newer than the running build. Passing
// nil (or never calling this) disables the check.
func (m Model) WithUpdateCheck(check func() (string, bool)) Model {
	m.checkUpdates = check
	return m
}

// updateCheckCmd runs the update-check off the UI goroutine, or nil when no
// checker is configured.
func (m Model) updateCheckCmd() tea.Cmd {
	if m.checkUpdates == nil {
		return nil
	}
	check := m.checkUpdates
	return func() tea.Msg {
		tag, ok := check()
		return updateCheckMsg{tag: tag, available: ok}
	}
}

// Init kicks off the first refresh, the status ticker, and (if present) the
// file-watch listener.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.refresh, tick()}
	if c := m.waitForChange(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.updateCheckCmd(); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

// overlayOpen reports whether a modal overlay is up. Used to defer hot reloads
// so an open overlay is never clobbered.
func (m Model) overlayOpen() bool {
	return m.modal != nil
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
		m.ensureVisible(paneKeys)
		m.ensureVisible(paneHosts)
		return m, nil

	case frameMsg:
		if !m.fxActive() {
			m.fx = activeFX{} // effect finished; stop the frame ticker
			return m, nil
		}
		return m, frameTick()

	case tickMsg:
		if m.status != "" && time.Since(m.statusSetAt) >= statusTTL {
			m.status = ""
		}
		// Apply a deferred reload once any overlay is closed.
		if m.pendingReload && !m.overlayOpen() && !m.loading {
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
		if m.overlayOpen() || m.loading {
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
			return m, m.play(false, false)
		}
		m.status = msg.verb
		// Re-sync so the loaded badge reflects the new agent state.
		m.loading = true
		return m, tea.Batch(m.refresh, m.play(true, false))

	case editDoneMsg:
		if msg.err != nil {
			m.status = "error: " + msg.err.Error()
			return m, m.play(false, false)
		}
		m.status = msg.verb
		m.muteReloadUntil = time.Now().Add(reloadMuteWindow) // our write, not external
		m.loading = true
		return m, tea.Batch(m.refresh, m.play(true, false))

	case keygenDoneMsg:
		if msg.err != nil {
			m.status = "ssh-keygen error: " + msg.err.Error()
			return m, m.play(false, false)
		}
		m.status = "key generated"
		m.muteReloadUntil = time.Now().Add(reloadMuteWindow)
		m.loading = true
		return m, tea.Batch(m.refresh, m.play(true, false))

	case restoreDoneMsg:
		if msg.err != nil {
			m.status = "restore failed: " + msg.err.Error()
			return m, m.play(false, false)
		}
		m.status = fmt.Sprintf("restored config from backup (%d file(s))", len(msg.files))
		m.muteReloadUntil = time.Now().Add(reloadMuteWindow)
		m.loading = true
		return m, tea.Batch(m.refresh, m.play(true, false))

	case connectDoneMsg:
		if msg.err != nil {
			m.status = "ssh " + msg.alias + ": " + msg.err.Error()
		} else {
			m.status = "session to " + msg.alias + " ended"
		}
		// The agent may have gained keys during the session (AddKeysToAgent).
		m.loading = true
		return m, m.refresh

	case clipDoneMsg:
		if msg.err != nil {
			m.status = "clipboard unavailable (install xclip/wl-clipboard): " + msg.err.Error()
		} else {
			m.status = "copied " + msg.label
		}
		return m, nil

	case updateCheckMsg:
		// Best-effort: only surface a positive result, never an error/no-op.
		if msg.available && msg.tag != "" {
			m.status = "update available: " + msg.tag + " — run `sshush update`"
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits, even from an overlay or the filter input.
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	// The active overlay captures input first (see overlay.go). It sees the same
	// padding-reduced dimensions View renders with, so scroll-clamping (e.g. the
	// help overlay's maxScroll) agrees with what's drawn. The reduced dims live on
	// a copy so the full size is preserved on the returned model.
	if m.modal != nil {
		sized := m.reduced()
		next, cmd := m.modal.Update(msg, &sized)
		// Overlays size against the reduced dims, but the full size is
		// authoritative — restore it before carrying state back so dims don't
		// compound across keystrokes on the persistent model.
		sized.width, sized.height = m.width, m.height
		m = sized
		m.modal = next
		return m, cmd
	}

	// The filter input (when focused) captures keys before pane navigation.
	if m.filtering {
		return m.handleFilterKey(msg)
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "/":
		m.filtering = true
		m.filterInput.Focus()
		return m, textinput.Blink
	case "esc":
		if m.filterQuery() != "" {
			m.filterInput.SetValue("")
			m.afterFilterChange()
			m.status = "filter cleared"
		}
		return m, nil
	case "tab", "left", "right", "h", "l":
		m.active = (m.active + 1) % numPanes
		m.clampCursor(m.active) // a global filter may shrink the new pane's list
		m.ensureVisible(m.active)
		return m, nil
	case "up", "k":
		m.moveCursor(m.active, -1)
		return m, nil
	case "down", "j":
		m.moveCursor(m.active, 1)
		return m, nil
	case "pgup", "ctrl+u":
		m.moveCursor(m.active, -m.listCapacity())
		return m, nil
	case "pgdown", "ctrl+d":
		m.moveCursor(m.active, m.listCapacity())
		return m, nil
	case "home", "g":
		m.setCursor(m.active, 0)
		return m, nil
	case "end", "G":
		m.setCursor(m.active, m.rowCount()-1)
		return m, nil
	case "enter", " ":
		if m.active == paneHosts {
			return m.connectToHost()
		}
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
	case "P":
		return m.beginPermsAudit()
	case "K":
		return m.beginKnownHosts()
	case "c":
		return m.beginCopy()
	case "?":
		m.modal = &helpOverlay{}
		return m, nil
	case "m":
		return m.toggleMotion()
	case "t":
		return m.beginTheme()
	case "R":
		return m.beginRestore()
	case "r":
		m.loading = true
		return m, m.refresh
	}
	return m, nil
}

// beginRestore opens the restore-from-backup confirm gate, or reports when no
// backup exists to revert to.
func (m Model) beginRestore() (tea.Model, tea.Cmd) {
	if !m.svc.CanRestore() {
		m.status = "no backup to restore (sshush writes one before its first edit)"
		return m, nil
	}
	m.modal = &restoreOverlay{}
	m.status = ""
	return m, nil
}

// sshCommandFor builds the shareable, explicit ssh invocation for a host from
// its own config block: port, identities, and options expanded as flags. It
// intentionally reflects only this block — wildcard/Match merging is ssh's job
// at connect time. Hosts with no HostName fall back to the alias form (with
// -F when a custom config is set), since there is nothing to expand.
func (m Model) sshCommandFor(h config.Host) string {
	if h.Hostname == "" {
		return "ssh " + strings.Join(m.sshArgs(firstAlias(h.Name)), " ")
	}
	parts := []string{"ssh"}
	if h.Port != 0 {
		parts = append(parts, "-p", fmt.Sprintf("%d", h.Port))
	}
	for _, id := range h.Identities {
		for _, ident := range m.ids {
			if ident.ID == id && ident.Path != "" {
				parts = append(parts, "-i", shellQuote(ident.Path))
				break
			}
		}
	}
	for _, k := range sortedOptionKeys(h.Options) {
		parts = append(parts, "-o", k+"="+shellQuote(h.Options[k]))
	}
	if h.User != "" {
		parts = append(parts, h.User+"@"+h.Hostname)
	} else {
		parts = append(parts, h.Hostname)
	}
	return strings.Join(parts, " ")
}

// shellQuote wraps s in single quotes if it contains characters the shell
// would interpret, so the copied command is safe to paste. Empty stays "”".
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("-_./:@%=+,", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	// single-quote, escaping embedded single quotes as '\''
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sortedOptionKeys returns a host's option names in sorted order (determinism).
func sortedOptionKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// beginCopy opens the clipboard copy menu with the options for the active pane.
func (m Model) beginCopy() (tea.Model, tea.Cmd) {
	var opts []copyOption
	if m.active == paneKeys {
		sel, ok := m.selectedKey()
		if !ok {
			m.status = "no key selected"
			return m, nil
		}
		if sel.PublicKey != "" {
			opts = append(opts, copyOption{"p", "public key", sel.PublicKey})
		}
		if sel.Fingerprint != "" {
			opts = append(opts, copyOption{"f", "fingerprint", sel.Fingerprint})
		}
	} else {
		sel, ok := m.selectedHost()
		if !ok {
			m.status = "no host selected"
			return m, nil
		}
		opts = append(opts, copyOption{"s", "ssh command", m.sshCommandFor(sel)})
	}
	if len(opts) == 0 {
		m.status = "nothing to copy"
		return m, nil
	}
	m.modal = &copyOverlay{opts: opts}
	m.status = ""
	return m, nil
}

// beginPermsAudit checks SSH file permissions and, if any are too permissive,
// opens a confirm-gated fix overlay.
func (m Model) beginPermsAudit() (tea.Model, tea.Cmd) {
	issues, err := m.svc.AuditPermissions()
	if err != nil {
		m.status = "permission audit: " + err.Error()
		return m, nil
	}
	if len(issues) == 0 {
		m.status = "permissions OK"
		return m, nil
	}
	m.modal = &permsOverlay{issues: issues}
	m.status = ""
	return m, nil
}

// beginKnownHosts loads known_hosts entries into a browsable overlay.
func (m Model) beginKnownHosts() (tea.Model, tea.Cmd) {
	entries, err := m.svc.KnownHosts()
	if err != nil {
		m.status = "known_hosts: " + err.Error()
		return m, nil
	}
	if len(entries) == 0 {
		m.status = "no known_hosts entries"
		return m, nil
	}
	m.modal = &knownHostsOverlay{entries: entries}
	m.status = ""
	return m, nil
}

// beginNew opens a creation flow: a new host on the Hosts pane, a new key on
// the Keys pane.
func (m Model) beginNew() (tea.Model, tea.Cmd) {
	m.status = ""
	if m.active == paneHosts {
		m.modal = newNewHostWizard()
		return m, textinput.Blink
	}
	m.modal = newNewKeyWizard()
	return m, nil
}

// beginDelete opens a confirm gate to delete the selected host or key.
func (m Model) beginDelete() (tea.Model, tea.Cmd) {
	if m.active == paneHosts {
		host, ok := m.selectedHost()
		if !ok {
			m.status = "no host to delete"
			return m, nil
		}
		if host.IsMatch {
			m.status = "Match block — read-only"
			return m, nil
		}
		m.modal = &deleteConfirmOverlay{host: host.ID}
		return m, nil
	}
	// Keys pane: only on-disk keys have files to delete.
	sel, ok := m.selectedKey()
	if !ok {
		return m, nil
	}
	if !sel.ExistsOnDisk || sel.Path == "" {
		m.status = "cannot delete agent-only key (no file on disk)"
		return m, nil
	}
	m.modal = &deleteConfirmOverlay{key: sel.ID}
	return m, nil
}

// beginEdit opens the edit overlay on the selected host. The editor's state and
// behaviour live in editOverlay (overlay_edit.go).
func (m Model) beginEdit() (tea.Model, tea.Cmd) {
	if m.active != paneHosts {
		return m, nil
	}
	host, ok := m.selectedHost()
	if !ok {
		m.status = "no host selected"
		return m, nil
	}
	if host.IsMatch {
		m.status = "Match block — read-only"
		return m, nil
	}
	m.modal = newEditOverlay(host)
	m.status = ""
	return m, textinput.Blink
}

// maybeAutoLoad loads the configured default identities into the agent once, on
// the first snapshot — those that exist on disk and aren't already loaded, in a
// single ssh-add invocation.
func (m Model) maybeAutoLoad() (tea.Model, tea.Cmd) {
	if m.autoLoaded || m.settings == nil || !m.settings.AutoLoad() {
		return m, nil
	}
	m.autoLoaded = true // attempt only once per session

	want := map[config.IdentityID]bool{}
	for _, id := range m.settings.DefaultIdentities() {
		want[id] = true
	}
	var paths []string
	for _, ident := range m.ids {
		if want[ident.ID] && ident.ExistsOnDisk && !ident.LoadedInAgent {
			paths = append(paths, ident.Path)
		}
	}
	if len(paths) == 0 {
		return m, nil
	}
	m.status = "loading default keys…"
	cmd := exec.Command("ssh-add", paths...)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return agentDoneMsg{verb: "default keys loaded", err: err}
	})
}

// setDefaultKey toggles the selected key's membership in the default set
// (Keys pane). On-disk keys only.
func (m Model) setDefaultKey() (tea.Model, tea.Cmd) {
	if m.active != paneKeys || m.settings == nil {
		if m.settings == nil {
			m.status = "no settings file configured"
		}
		return m, nil
	}
	sel, ok := m.selectedKey()
	if !ok {
		return m, nil
	}
	if !sel.ExistsOnDisk && !m.settings.IsDefault(sel.ID) {
		m.status = "cannot set agent-only key as default"
		return m, nil
	}
	added, err := m.settings.ToggleDefault(sel.ID)
	if err != nil {
		m.status = "settings error: " + err.Error()
		return m, nil
	}
	if added {
		m.status = "added default: " + sel.Name
		if _, any := shellinit.Installed(); !any {
			m.status += " · tip: sshush shell-init >> ~/.bashrc to load on shell start"
		}
	} else {
		m.status = "removed default: " + sel.Name
	}
	return m, nil
}

// toggleMotion flips the motion system on/off and persists the choice.
func (m Model) toggleMotion() (tea.Model, tea.Cmd) {
	if m.settings == nil {
		m.status = "no settings file configured"
		return m, nil
	}
	on := !m.settings.MotionEnabled()
	if err := m.settings.SetMotion(on, ""); err != nil {
		m.status = "settings error: " + err.Error()
		return m, nil
	}
	if on {
		m.status = "motion on (" + m.settings.MotionIntensity() + ") — press m to disable"
		return m, m.play(true, true) // demo: flash + shake so the level is obvious
	}
	m.status = "motion off"
	m.fx = activeFX{}
	return m, nil
}

// beginTheme opens the theme picker, previewing the current theme. The picker's
// state and behaviour live in themeOverlay (overlay_theme.go).
func (m Model) beginTheme() (tea.Model, tea.Cmd) {
	if m.settings == nil {
		m.status = "no settings file configured"
		return m, nil
	}
	o := &themeOverlay{orig: m.settings.ThemeName()}
	for i, p := range presets {
		if p.name == o.orig {
			o.cursor = i
			break
		}
	}
	applyTheme(presets[o.cursor].theme)
	m.modal = o
	m.status = ""
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
	v := m.visibleHosts()
	i := m.vp[paneHosts].cursor
	if i < 0 || i >= len(v) {
		return config.Host{}, false
	}
	return v[i], true
}

// firstAlias returns the first pattern of a host's name (a block may declare
// several, e.g. "web prod-web"); ssh connects by a single alias.
func firstAlias(name string) string {
	if fields := strings.Fields(name); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// connectToHost launches `ssh <alias>` for the selected host via ExecProcess,
// yielding the terminal for the session and resuming sshush when it exits. The
// user's own config (ProxyJump, IdentityFile, …) applies. Wildcard blocks can't
// be connected to.
func (m Model) connectToHost() (tea.Model, tea.Cmd) {
	host, ok := m.selectedHost()
	if !ok {
		return m, nil
	}
	if host.IsMatch {
		m.status = "Match block — read-only (cannot connect)"
		return m, nil
	}
	if host.IsPattern {
		m.status = "cannot connect to a wildcard/pattern host"
		return m, nil
	}
	alias := firstAlias(host.Name)
	if alias == "" {
		m.status = "host has no alias to connect to"
		return m, nil
	}
	m.status = "connecting to " + alias + "…"
	cmd := exec.Command("ssh", m.sshArgs(alias)...)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return connectDoneMsg{alias: alias, err: err}
	})
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
	if host.IsMatch {
		m.status = "Match block — read-only"
		return m, nil
	}
	if len(m.diskKeys()) == 0 {
		m.status = "no on-disk keys to associate"
		return m, nil
	}
	m.modal = &pickerOverlay{host: host.ID}
	m.status = ""
	return m, nil
}

// toggleSelectedKey loads or unloads the highlighted key in the agent via
// ssh-add, run through tea.ExecProcess so the terminal is free for a passphrase
// prompt. Only applies on the Keys pane to on-disk keys.
func (m Model) toggleSelectedKey() (tea.Model, tea.Cmd) {
	if m.active != paneKeys {
		return m, nil
	}
	sel, ok := m.selectedKey()
	if !ok {
		return m, nil
	}
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
	m.ensureVisible(paneKeys)
	m.ensureVisible(paneHosts)
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
	if m.sshDir != "" {
		add(m.sshDir)
	} else if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".ssh"))
	}
	_ = m.watcher.Watch(dirs)
}

// listCapacity is how many data rows fit in a pane given the terminal height.
// When the height is unknown (e.g. before the first WindowSizeMsg, or in tests)
// it returns a large number so every row renders.
func (m Model) listCapacity() int {
	if m.height <= 0 {
		return 1 << 30 // height unknown (initial render / tests): show all rows
	}
	// Fixed chrome: header+tabs (1), box borders (2), column header (1),
	// scroll-indicator reserve (1), plus the help lines. Then add the optional
	// lines actually present so the list shrinks to keep everything — header
	// included — on screen, and grows when they're absent.
	chrome := 1 + 2 + 1 + 1 + len(m.helpGroups())
	if m.status != "" {
		chrome++
	}
	if m.filtering || m.filterQuery() != "" {
		chrome++
	}
	if m.srcFile != "" {
		chrome++
	}
	if c := m.height - chrome; c > 1 {
		return c
	}
	return 1
}

// moveCursor shifts the cursor by delta (clamped) and keeps it visible.
// moveCursor / setCursor / ensureVisible / window / clampCursor bind pane p's
// viewport to its row count and the screen-dependent list capacity — the Model
// is the adapter that supplies capacity; the arithmetic lives in viewport.go.

func (m *Model) moveCursor(p pane, delta int) {
	m.vp[p].moveCursor(delta, m.rowCountFor(p), m.listCapacity())
}

func (m *Model) setCursor(p pane, i int) {
	m.vp[p].setCursor(i, m.rowCountFor(p), m.listCapacity())
}

// ensureVisible scrolls pane p so its cursor row is within the viewport.
func (m *Model) ensureVisible(p pane) {
	m.vp[p].ensureVisible(m.rowCountFor(p), m.listCapacity())
}

// window returns the [start, end) row range visible for pane p.
func (m Model) window(p pane) (int, int) {
	return m.vp[p].window(m.rowCountFor(p), m.listCapacity())
}

// scrollIndicator is a dim "rows X–Y of N" line shown when a pane overflows.
func (m Model) scrollIndicator(p pane) string {
	n := m.rowCountFor(p)
	start, end := m.window(p)
	if start == 0 && end == n {
		return "" // everything fits
	}
	arrows := ""
	if start > 0 {
		arrows += " ↑"
	}
	if end < n {
		arrows += " ↓"
	}
	return dimStyle.Render(fmt.Sprintf("  rows %d–%d of %d%s", start+1, end, n, arrows))
}

func (m *Model) clampCursors() {
	for p := pane(0); p < numPanes; p++ {
		m.clampCursor(p)
	}
}

// clampCursor keeps pane p's cursor within [0, rowCount).
func (m *Model) clampCursor(p pane) {
	m.vp[p].clampCursor(m.rowCountFor(p))
}

func (m Model) rowCount() int { return m.rowCountFor(m.active) }

func (m Model) rowCountFor(p pane) int {
	if p == paneKeys {
		return len(m.visibleIDs())
	}
	return len(m.visibleHosts())
}

// --- styles (basic; polish pass is a later milestone) ---

var (
	// Palette colors and styles — all assigned by applyTheme (see theme.go),
	// so the look is swappable at runtime.
	colPrimary, colAccent, colGreen, colDim lipgloss.Color
	colErr, colGold, colBorder, colSelBg    lipgloss.Color
	colBg                                   lipgloss.Color

	appTitleStyle, titleStyle, tabActive        lipgloss.Style
	tabSelected, tabUnselected, headerStyle     lipgloss.Style
	selectedRow, loadedBadge, textStyle         lipgloss.Style
	dimStyle, errStyle, starStyle, keyCap       lipgloss.Style
	statusStyle, boxStyle, helpKey, helpLabel   lipgloss.Style
	hostTagStyle, flashGoodStyle, flashBadStyle lipgloss.Style
)

// padding is the breathing room around the whole app: 1 row top/bottom and
// 2 cols left/right, dropped entirely on small terminals so content wins.
func (m Model) padding() (x, y int) {
	if m.width >= 40 && m.height >= 12 {
		return 2, 1
	}
	return 0, 0
}

// reduced returns a copy of m with the app padding subtracted from its
// dimensions — the view the inner panes and overlays are sized against.
// The full m.width/m.height remain authoritative; only this copy is shrunk.
func (m Model) reduced() Model {
	padX, padY := m.padding()
	m.width -= 2 * padX
	m.height -= 2 * padY
	return m
}

// applyPadding insets s by x columns and y blank rows (no trailing newline).
func applyPadding(s string, x, y int) string {
	lines := strings.Split(s, "\n")
	pad := strings.Repeat(" ", x)
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	blank := make([]string, y)
	out := append(append(blank, lines...), make([]string, y)...)
	return strings.Join(out, "\n")
}

// View renders the current screen on a model copy whose dimensions are reduced
// by the padding, then layers the padding, the theme background (at full size,
// so it covers the padding), and any active screen-shake over the whole output
// (so overlays get them too).
func (m Model) View() string {
	padX, padY := m.padding()
	inner := m.reduced()
	out := inner.viewInner()
	if padX > 0 || padY > 0 {
		out = applyPadding(out, padX, padY)
	}
	out = applyBackground(out, m.width, m.height)
	if m.fxActive() && m.fx.shakeAmp > 0 {
		out = m.applyShake(out)
	}
	return out
}

// viewInner renders the current pane or active overlay (no bg/shake).
func (m Model) viewInner() string {
	if m.modal != nil {
		return m.card(m.modal.View(&m))
	}

	header := appTitleStyle.Render("sshush") + "   " + m.renderTabs()

	var body string
	switch {
	case m.loading:
		body = m.box(dimStyle.Render("loading…"))
	case m.err != nil:
		body = m.box(errStyle.Render("error: " + m.err.Error()))
	default:
		body = m.box(strings.Join(m.paneLines(m.active, m.boxInner()), "\n"))
	}

	var f strings.Builder
	f.WriteString(header + "\n")
	f.WriteString(body + "\n")
	if m.filtering {
		f.WriteString("  " + helpKey.Render("/") + " " + m.filterInput.View() + "\n")
	} else if q := m.filterQuery(); q != "" {
		f.WriteString(statusStyle.Render(fmt.Sprintf("  filter: %s", q)) +
			dimStyle.Render(fmt.Sprintf("  (%d match · esc clears)", m.rowCountFor(m.active))) + "\n")
	}
	if m.status != "" {
		f.WriteString(m.renderStatus() + "\n")
	}
	f.WriteString(m.renderHelp())
	if m.srcFile != "" {
		f.WriteString("\n" + dimStyle.Render("  "+m.srcFile))
	}
	// No trailing newline: an extra blank line would push the header off the top
	// of an exactly-full screen. (Background + shake are layered in View.)
	return f.String()
}

// renderStatus draws the status line, as a full-width flash bar while a flash
// effect is playing (so the highlight fills the row instead of hugging text).
func (m Model) renderStatus() string {
	text := "  " + m.status
	if m.fxActive() && (m.fx.flashGood || m.fx.flashBad) {
		fs := flashGoodStyle
		if m.fx.flashBad {
			fs = flashBadStyle
		}
		if m.width > 0 {
			fs = fs.Width(m.width)
		}
		return fs.Render(text)
	}
	return statusStyle.Render(text)
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

// boxInner is the content width inside the single full-width pane box. The box
// uses Width(m.width-2) (lipgloss Width includes padding), so the text area is
// that minus the 2-col padding.
func (m Model) boxInner() int {
	if m.width <= 6 {
		return 0 // unknown/tiny: callers treat <=0 as "don't truncate"
	}
	return m.width - 4
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

// fit truncates a (possibly ANSI-styled) string to width w with an ellipsis.
// w<=0 means width is unknown — leave it untouched.
func fit(s string, w int) string {
	if w <= 0 {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// card wraps an overlay in a rounded border, separated from the top edge.
func (m Model) card(content string) string {
	return "\n" + boxStyle.Render(strings.TrimRight(content, "\n")) + "\n"
}

// helpSection is one titled block of key/description rows in the help overlay.
type helpSection struct {
	title string
	items []helpItem
}

var helpReference = []helpSection{
	{"Navigation", []helpItem{
		{"↑/↓ k/j", "move"},
		{"PgUp/PgDn", "page (also ctrl+u/ctrl+d)"},
		{"g / G", "top / bottom"},
		{"tab ←/→", "switch panes (also h/l)"},
		{"/", "filter the active pane (esc clears)"},
		{"r", "refresh"},
		{"? ", "this help"},
		{"q / ctrl+c", "quit"},
	}},
	{"Keys pane", []helpItem{
		{"enter/space", "load / unload key in the agent"},
		{"c", "copy public key / fingerprint"},
		{"s", "toggle key in/out of startup defaults"},
		{"U", "unload all keys from the agent"},
		{"n", "generate a new key"},
		{"d", "delete the key files (irreversible)"},
	}},
	{"Hosts pane", []helpItem{
		{"enter", "ssh into the host"},
		{"c", "copy ssh command"},
		{"e", "edit host directives"},
		{"i", "attach / detach keys"},
		{"n", "add a host (wizard)"},
		{"d", "delete the host"},
	}},
	{"Tools", []helpItem{
		{"P", "audit & fix file permissions"},
		{"K", "browse / remove known_hosts entries"},
		{"R", "restore config from backup (undo edits)"},
		{"m", "toggle motion / animations"},
		{"t", "switch theme"},
	}},
	{"In overlays", []helpItem{
		{"esc", "cancel / close"},
		{"y / n", "confirm / decline a write"},
		{"tab", "next field (edit) / next algorithm (new key)"},
		{"ctrl+o", "add option (host edit)"},
		{"ctrl+d", "delete directive (host edit)"},
	}},
}

// sectionLines renders a help section to display lines (title + rows + blank).
func sectionLines(sec helpSection) []string {
	ls := []string{helpLabel.Render(sec.title)}
	for _, it := range sec.items {
		ls = append(ls, "  "+helpKey.Render(fmt.Sprintf("%-12s", it.key))+" "+dimStyle.Render(it.desc))
	}
	return append(ls, "")
}

// helpLines builds the single-column reference body.
func (m Model) helpLines() []string {
	var out []string
	for _, s := range helpReference {
		out = append(out, sectionLines(s)...)
	}
	return out
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
	view := helpGroup{"view", []helpItem{{"/", "filter"}, {"P", "perms"}, {"K", "known_hosts"}, {"R", "restore"}, {"t", "theme"}, {"m", "motion"}, {"?", "help"}, {"tab", "panes"}, {"q", "quit"}}}
	if m.active == paneKeys {
		return []helpGroup{
			{"agent", []helpItem{{"↵", "load/unload"}, {"U", "unload all"}, {"s", "default"}}},
			{"keys", []helpItem{{"c", "copy"}, {"n", "new"}, {"d", "delete"}}},
			view,
		}
	}
	return []helpGroup{
		{"hosts", []helpItem{{"↵", "connect"}, {"c", "copy"}, {"e", "edit"}, {"i", "keys"}, {"n", "new"}, {"d", "delete"}}},
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
		lines = append(lines, fit("  "+label+strings.Join(parts, sep), m.width))
	}
	return strings.Join(lines, "\n")
}

// loadGlyph is a filled circle for keys present in the agent, a hollow one for
// keys that are not.
const (
	glyphLoaded   = "●"
	glyphUnloaded = "○"
)

// paneLines renders a pane's content (header, windowed rows, scroll indicator)
// as display lines, with rows truncated to innerWidth.
func (m Model) paneLines(p pane, w int) []string {
	if p == paneKeys {
		return m.keysLines(w)
	}
	return m.hostsLines(w)
}

func (m Model) keysLines(w int) []string {
	vis := m.visibleIDs()
	if len(m.ids) == 0 {
		return []string{dimStyle.Render("no keys found")}
	}
	if len(vis) == 0 {
		return []string{dimStyle.Render("no keys match " + strconv.Quote(m.filterQuery()))}
	}
	usedBy := m.hostsByKey()
	start, end := m.window(paneKeys)

	// hosts column width: widest visible tag, clamped to [5, 28], so the comment
	// column starts at the same offset on every row and in the header.
	hostsW := 5
	for i := start; i < end; i++ {
		if hosts := usedBy[vis[i].ID]; len(hosts) > 0 {
			if l := lipgloss.Width("↪ " + strings.Join(hosts, ", ")); l > hostsW {
				hostsW = l
			}
		}
	}
	if hostsW > 28 {
		hostsW = 28
	}

	// Header is built from the SAME widths as the rows so it cannot drift:
	// listRow prefix (2) + gutter "● ★ " (4) + name(20)+1 + algo(11)+1 +
	// hosts(hostsW)+2 + comment. The ★ slot is always reserved (blank when not
	// default) so the name column never shifts.
	header := strings.Repeat(" ", 2+4) +
		padClip("name", 20) + " " + padClip("algo", 11) + " " +
		padClip("hosts", hostsW) + "  comment"
	lines := []string{fit(headerStyle.Render(header), w)}

	for i := start; i < end; i++ {
		id := vis[i]
		glyph := glyphUnloaded
		glyphStyle := dimStyle
		if id.LoadedInAgent {
			glyph, glyphStyle = glyphLoaded, loadedBadge
		}
		star, starStyled := " ", " "
		if m.settings != nil && m.settings.IsDefault(id.ID) {
			star, starStyled = "★", starStyle.Render("★")
		}
		algo := string(id.Algorithm)
		if !id.ExistsOnDisk {
			algo = "agent-only"
		}
		tag := ""
		if hosts := usedBy[id.ID]; len(hosts) > 0 {
			tag = "↪ " + strings.Join(hosts, ", ")
		}

		// Order: gutter (glyph, star), name, algo, hosts, comment. The comment is
		// last so width-clipping in listRow/fit drops it first; hosts gets its own
		// padClip ellipsis. Columns never shift because every slot is fixed-width.
		nameCol := padClip(id.Name, 20)
		algoCol := padClip(algo, 11)
		hostsCol := padClip(tag, hostsW)
		plain := glyph + " " + star + " " + nameCol + " " + algoCol + " " + hostsCol + "  " + id.Comment
		styled := glyphStyle.Render(glyph) + " " + starStyled + " " +
			textStyle.Render(nameCol) + " " + dimStyle.Render(algoCol) + " " +
			hostTagStyle.Render(hostsCol) + "  " + dimStyle.Render(id.Comment)
		lines = append(lines, m.listRow(paneKeys, i, plain, styled, w))
	}
	if ind := m.scrollIndicator(paneKeys); ind != "" {
		lines = append(lines, fit(ind, w))
	}
	return lines
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

func (m Model) hostsLines(w int) []string {
	vis := m.visibleHosts()
	if len(m.hosts) == 0 {
		return []string{dimStyle.Render("no hosts found")}
	}
	if len(vis) == 0 {
		return []string{dimStyle.Render("no hosts match " + strconv.Quote(m.filterQuery()))}
	}
	lines := []string{fit(headerStyle.Render(strings.Repeat(" ", 2)+padClip("host", 20)+" destination"), w)}
	start, end := m.window(paneHosts)
	for i := start; i < end; i++ {
		h := vis[i]
		// Match blocks are surfaced read-only: the name column shows the match
		// pattern (the "match" tag marks the block type) so it lines up with the
		// destination column exactly like a normal host row.
		if h.IsMatch {
			nameCol := padClip(matchLabel(h.MatchCriteria), 20)
			plain := nameCol + " match · read-only"
			styled := textStyle.Render(nameCol) + " " + dimStyle.Render("match · ") + hostTagStyle.Render("read-only")
			lines = append(lines, m.listRow(paneHosts, i, plain, styled, w))
			continue
		}
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
		nameCol := padClip(h.Name, 20)
		plain := nameCol + " " + dest
		styled := textStyle.Render(nameCol) + " " + dimStyle.Render(dest)
		lines = append(lines, m.listRow(paneHosts, i, plain, styled, w))
	}
	if ind := m.scrollIndicator(paneHosts); ind != "" {
		lines = append(lines, fit(ind, w))
	}
	return lines
}

// matchLabel reduces a Match criteria string to its pattern for the name column,
// dropping the "Match"/"Host" keywords (the row's "match" tag conveys the type):
// "Match Host *.corp" → "*.corp"; "Match all" → "all".
func matchLabel(crit string) string {
	s := strings.TrimPrefix(crit, "Match ")
	s = strings.TrimPrefix(s, "Host ")
	return s
}

// padClip pads s with spaces to exactly n columns, or truncates it with an
// ellipsis when it is wider, so fixed-column rows always line up.
func padClip(s string, n int) string {
	if n <= 0 {
		return s
	}
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return ansi.Truncate(s, n, "…")
}

// listRow renders one list row, truncated to width w. The active pane's cursor
// row uses the plain text with a full-width highlight (embedding pre-styled
// spans would reset the background mid-row); other rows use the styled text.
// The inactive pane's cursor still gets a dim marker so its position is visible.
func (m Model) listRow(p pane, i int, plain, styled string, w int) string {
	if p == m.active && i == m.vp[p].cursor {
		s := selectedRow
		if w > 0 {
			s = s.Width(w)
		}
		return s.Render(fit("▸ "+plain, w))
	}
	prefix := "  "
	if i == m.vp[p].cursor {
		prefix = dimStyle.Render("▸ ") // inactive pane cursor
	}
	return prefix + fit(styled, w-2)
}
