package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/s-johri/sshush/internal/config"
)

// hostSteps drives the new-host wizard's basic phase: an alias (required) then
// optional basic fields. Empty answers are skipped.
var hostSteps = []struct{ field, hint string }{
	{"alias", "host alias (e.g. prod-web) — required"},
	{"HostName", "hostname / IP (optional, enter to skip)"},
	{"User", "user (optional, enter to skip)"},
	{"Port", "port (optional number, enter to skip)"},
}

// newHostWizard creates a host block: the basic fields (hostSteps), then a loop
// of optional custom options (name → value; blank name finishes and dispatches
// AddHost). One overlay, private phase (see CONTEXT.md).
type newHostWizard struct {
	phase  int // nhPhaseBasics walks hostSteps; then option name/value loop
	step   int // index into hostSteps while in nhPhaseBasics
	draft  config.Host
	optKey string // custom option name awaiting its value
	input  textinput.Model
}

const (
	nhPhaseBasics = iota
	nhPhaseOptKey
	nhPhaseOptVal
	nhPhaseConfirm // y/n before AddHost writes the block
)

// newNewHostWizard starts the wizard at the alias step.
func newNewHostWizard() *newHostWizard {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.SetWidth(40)
	styleTextInput(&ti)
	ti.Focus()
	return &newHostWizard{input: ti}
}

// Paste adds pasted text to the input on the text steps (not the y/n step).
func (o *newHostWizard) Paste(msg tea.PasteMsg) tea.Cmd {
	if o.phase == nhPhaseConfirm {
		return nil
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	return cmd
}

func (o *newHostWizard) Update(msg tea.KeyPressMsg, m *Model) (overlay, tea.Cmd) {
	if msg.String() == "esc" {
		m.status = "cancelled"
		return nil, nil
	}
	if o.phase == nhPhaseConfirm {
		if msg.String() != "y" && msg.String() != "Y" {
			m.status = "cancelled"
			return nil, nil
		}
		host := o.draft
		m.status = "creating host…"
		return nil, func() tea.Msg { return editDoneMsg{verb: "host added", err: m.svc.AddHost(host)} }
	}
	if msg.String() != "enter" {
		var cmd tea.Cmd
		o.input, cmd = o.input.Update(msg)
		return o, cmd
	}
	val := strings.TrimSpace(o.input.Value())
	switch o.phase {
	case nhPhaseBasics:
		return o.enterBasic(val, m)
	case nhPhaseOptKey:
		return o.enterOptKey(val, m)
	default:
		return o.enterOptVal(val, m)
	}
}

// enterBasic records one basic field (skipping empties), advancing to the
// custom-options loop after the last step.
func (o *newHostWizard) enterBasic(val string, m *Model) (overlay, tea.Cmd) {
	switch hostSteps[o.step].field {
	case "alias":
		if err := config.ValidateAlias(val); err != nil {
			m.status = err.Error()
			return o, nil
		}
		o.draft.ID = config.HostID(val)
		o.draft.Name = val
	case "HostName", "User", "Port":
		if val == "" {
			break // optional: skip
		}
		field := hostSteps[o.step].field
		if err := config.ValidateValue(field, val); err != nil {
			m.status = err.Error()
			return o, nil
		}
		switch field {
		case "HostName":
			o.draft.Hostname = val
		case "User":
			o.draft.User = val
		default:
			o.draft.Port, _ = strconv.Atoi(val) // checked above
		}
	}
	if o.step == len(hostSteps)-1 {
		o.phase = nhPhaseOptKey
	} else {
		o.step++
	}
	o.input.SetValue("")
	return o, nil
}

// enterOptKey collects a custom option name; a blank name goes to the y/n
// step.
func (o *newHostWizard) enterOptKey(key string, m *Model) (overlay, tea.Cmd) {
	if key == "" {
		o.phase = nhPhaseConfirm
		o.input.Blur()
		return o, nil
	}
	if err := config.ValidateOption(key); err != nil {
		m.status = err.Error()
		return o, nil
	}
	o.optKey = key
	o.phase = nhPhaseOptVal
	o.input.SetValue("")
	return o, nil
}

// enterOptVal stores a custom option value, then loops back for more.
func (o *newHostWizard) enterOptVal(val string, m *Model) (overlay, tea.Cmd) {
	if err := config.ValidateValue(o.optKey, val); err != nil {
		m.status = err.Error()
		return o, nil
	}
	if o.draft.Options == nil {
		o.draft.Options = map[string]string{}
	}
	o.draft.Options[o.optKey] = val
	m.status = o.optKey + " added"
	o.optKey = ""
	o.phase = nhPhaseOptKey
	o.input.SetValue("")
	return o, nil
}

func (o *newHostWizard) View(m *Model) string {
	switch o.phase {
	case nhPhaseBasics:
		step := hostSteps[o.step]
		title := fmt.Sprintf("New host (%d/%d)", o.step+1, len(hostSteps))
		return o.prompt(title, step.field+" — "+step.hint)
	case nhPhaseOptKey:
		return o.prompt("New host: add option",
			"option name (e.g. ForwardAgent) — enter blank to finish")
	case nhPhaseConfirm:
		return o.viewConfirm(m)
	default:
		return o.prompt("New host: add option", o.optKey+" value")
	}
}

// viewConfirm shows the block that AddHost will write, and asks y/n.
func (o *newHostWizard) viewConfirm(m *Model) string {
	var b strings.Builder
	b.WriteString(tabActive.Render("Confirm new host") + "\n\n")
	lines := []string{"Host " + o.draft.Name}
	if o.draft.Hostname != "" {
		lines = append(lines, "    HostName "+o.draft.Hostname)
	}
	if o.draft.User != "" {
		lines = append(lines, "    User "+o.draft.User)
	}
	if o.draft.Port != 0 {
		lines = append(lines, "    Port "+strconv.Itoa(o.draft.Port))
	}
	keys := make([]string, 0, len(o.draft.Options))
	for k := range o.draft.Options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, "    "+k+" "+o.draft.Options[k])
	}
	for _, l := range lines {
		b.WriteString("  " + textStyle.Render(l) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("  (a backup of the config file is written first)") + "\n")
	b.WriteString(m.rewriteNote("") + "\n")
	b.WriteString("  " + keyCap.Render("y") + textStyle.Render(" write    ") + keyCap.Render("n") + textStyle.Render(" cancel"))
	b.WriteString("\n")
	return b.String()
}

// prompt renders a single-line text prompt for the current step.
func (o *newHostWizard) prompt(title, hint string) string {
	var b strings.Builder
	b.WriteString(tabActive.Render(title) + "\n\n")
	b.WriteString(dimStyle.Render("  "+hint) + "\n")
	b.WriteString("  " + o.input.View() + "\n\n")
	b.WriteString(dimStyle.Render("  enter confirm · esc cancel"))
	b.WriteString("\n")
	return b.String()
}
