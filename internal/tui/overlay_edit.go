package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/pkg/config"
)

// editOverlay edits a host's directives. One overlay, four private phases (see
// CONTEXT.md): value entry over the existing fields (tab cycles, ctrl+o adds an
// option, ctrl+d deletes), option-name entry for a new directive, and the
// confirm gates for writes and deletes.
type editOverlay struct {
	phase    int
	host     config.HostID
	fields   []string // core fields + the host's existing option keys
	fieldIdx int      // index into fields
	newKey   string   // option name being added (edPhaseOptName → edPhaseValue)
	input    textinput.Model
}

const (
	edPhaseValue      = iota // typing a value for the active field
	edPhaseOptName           // typing a new option name
	edPhaseConfirm           // confirming a write
	edPhaseConfirmDel        // confirming a directive removal
)

// newEditOverlay opens the editor on host: only directives the host actually
// has are listed; absent ones (e.g. Port) are added via ctrl+o.
func newEditOverlay(host config.Host) *editOverlay {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 40
	o := &editOverlay{host: host.ID, fields: presentFields(host)}
	o.input = ti
	o.input.SetValue(o.currentFieldValue(host))
	o.input.CursorEnd()
	o.input.Focus()
	return o
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

// activeField is the directive being edited: a newly typed option name, else
// the currently selected existing field.
func (o *editOverlay) activeField() string {
	if o.newKey != "" {
		return o.newKey
	}
	if o.fieldIdx < len(o.fields) {
		return o.fields[o.fieldIdx]
	}
	return ""
}

// currentFieldValue returns host's value for the active field.
func (o *editOverlay) currentFieldValue(host config.Host) string {
	switch o.activeField() {
	case "HostName":
		return host.Hostname
	case "User":
		return host.User
	case "Port":
		if host.Port == 0 {
			return ""
		}
		return strconv.Itoa(host.Port)
	default:
		return host.Options[o.activeField()]
	}
}

func (o *editOverlay) Update(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	switch o.phase {
	case edPhaseValue:
		return o.updateValue(msg, m)
	case edPhaseOptName:
		return o.updateOptName(msg, m)
	default:
		return o.updateConfirm(msg, m)
	}
}

// updateValue drives value entry. tab cycles fields (existing edits only);
// ctrl+d deletes the active directive; enter confirms.
func (o *editOverlay) updateValue(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	editingExisting := o.newKey == "" && len(o.fields) > 0
	switch msg.String() {
	case "esc":
		m.status = "edit cancelled"
		return nil, nil
	case "ctrl+o": // add a new option (works whether or not fields exist)
		if o.newKey == "" {
			o.phase = edPhaseOptName
			o.input.SetValue("")
			o.input.Focus()
			return o, textinput.Blink
		}
	case "tab":
		if editingExisting { // cycling only applies to existing fields
			o.fieldIdx = (o.fieldIdx + 1) % len(o.fields)
			if host, ok := m.hostByID(o.host); ok {
				o.input.SetValue(o.currentFieldValue(host))
				o.input.CursorEnd()
			}
		}
		return o, nil
	case "ctrl+d":
		if editingExisting {
			o.phase = edPhaseConfirmDel
		}
		return o, nil
	case "enter":
		if o.newKey == "" && len(o.fields) == 0 {
			m.status = "no directives set — ctrl+o to add one"
			return o, nil
		}
		if strings.TrimSpace(o.input.Value()) == "" {
			m.status = "value cannot be empty (ctrl+d to delete a directive)"
			return o, nil
		}
		o.phase = edPhaseConfirm
		return o, nil
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	return o, cmd
}

// updateOptName collects an option name, then transitions to value entry.
func (o *editOverlay) updateOptName(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.status = "add cancelled"
		return nil, nil
	case "enter":
		key := strings.TrimSpace(o.input.Value())
		if key == "" {
			m.status = "option name cannot be empty"
			return o, nil
		}
		o.newKey = key
		o.phase = edPhaseValue
		o.input.SetValue("")
		return o, nil
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	return o, cmd
}

// updateConfirm gates writes and deletes: only "y" proceeds.
func (o *editOverlay) updateConfirm(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	if msg.String() != "y" && msg.String() != "Y" {
		m.status = "cancelled"
		return nil, nil
	}
	host := o.host
	field := o.activeField()
	m.status = "writing…"
	if o.phase == edPhaseConfirmDel {
		return nil, func() tea.Msg {
			return editDoneMsg{verb: "removed", err: m.svc.DeleteHostField(host, field)}
		}
	}
	val := strings.TrimSpace(o.input.Value())
	return nil, func() tea.Msg {
		return editDoneMsg{verb: "saved", err: m.svc.EditHost(host, field, val)}
	}
}

func (o *editOverlay) View(m *Model) string {
	switch o.phase {
	case edPhaseOptName:
		return o.viewOptName()
	case edPhaseConfirm, edPhaseConfirmDel:
		return o.viewConfirm(m)
	default:
		return o.viewValue()
	}
}

func (o *editOverlay) viewValue() string {
	var b strings.Builder
	title := "Edit host: " + string(o.host)
	if o.newKey != "" {
		title = "Add " + o.newKey + " to " + string(o.host)
	}
	b.WriteString(tabActive.Render(title) + "\n\n")
	switch {
	case o.newKey != "":
		b.WriteString("  " + o.newKey + "\n")
		b.WriteString("  " + o.input.View() + "\n\n")
		b.WriteString(dimStyle.Render("  enter confirm · esc cancel"))
	case len(o.fields) == 0:
		b.WriteString(dimStyle.Render("  (no directives set)") + "\n\n")
		b.WriteString(dimStyle.Render("  ctrl+o add option · esc cancel"))
	default:
		b.WriteString("  " + o.activeField() + "\n")
		b.WriteString("  " + o.input.View() + "\n\n")
		b.WriteString(dimStyle.Render("  tab next · ctrl+o add option · ctrl+d delete · enter confirm · esc cancel"))
	}
	b.WriteString("\n")
	return b.String()
}

func (o *editOverlay) viewOptName() string {
	var b strings.Builder
	b.WriteString(tabActive.Render("Add option to "+string(o.host)) + "\n\n")
	b.WriteString("  option name (e.g. ForwardAgent)\n")
	b.WriteString("  " + o.input.View() + "\n\n")
	b.WriteString(dimStyle.Render("  enter next · esc cancel"))
	b.WriteString("\n")
	return b.String()
}

func (o *editOverlay) viewConfirm(m *Model) string {
	field := o.activeField()
	var b strings.Builder
	if o.phase == edPhaseConfirmDel {
		b.WriteString(errStyle.Render("Confirm delete") + "\n\n")
		b.WriteString(fmt.Sprintf("  Remove %s from %s\n", field, o.host))
	} else {
		val := strings.TrimSpace(o.input.Value())
		b.WriteString(tabActive.Render("Confirm write") + "\n\n")
		b.WriteString(fmt.Sprintf("  Set %s of %s to %q\n", field, o.host, val))
	}
	if h, ok := m.hostByID(o.host); ok && h.IsPattern {
		b.WriteString(errStyle.Render("  this is a wildcard block — affects every matching connection") + "\n")
	}
	b.WriteString(dimStyle.Render("  (a .bak backup of the config file is written first)") + "\n\n")
	b.WriteString("  " + keyCap.Render("y") + " write    " + keyCap.Render("n") + " cancel")
	b.WriteString("\n")
	return b.String()
}
