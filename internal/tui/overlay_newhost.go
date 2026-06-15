package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/pkg/config"
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
)

// newNewHostWizard starts the wizard at the alias step.
func newNewHostWizard() *newHostWizard {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 40
	ti.PromptStyle = textStyle
	ti.TextStyle = textStyle
	ti.Focus()
	return &newHostWizard{input: ti}
}

func (o *newHostWizard) Update(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	if msg.String() == "esc" {
		m.status = "cancelled"
		return nil, nil
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
		if val == "" {
			m.status = "host alias cannot be empty"
			return o, nil
		}
		o.draft.ID = config.HostID(val)
		o.draft.Name = val
	case "HostName":
		if val != "" {
			o.draft.Hostname = val
		}
	case "User":
		if val != "" {
			o.draft.User = val
		}
	case "Port":
		if val != "" {
			p, err := strconv.Atoi(val)
			if err != nil {
				m.status = "port must be a number"
				return o, nil
			}
			o.draft.Port = p
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

// enterOptKey collects a custom option name; a blank name finishes the wizard
// and dispatches AddHost.
func (o *newHostWizard) enterOptKey(key string, m *Model) (overlay, tea.Cmd) {
	if key == "" {
		host := o.draft
		m.status = "creating host…"
		return nil, func() tea.Msg { return editDoneMsg{verb: "host added", err: m.svc.AddHost(host)} }
	}
	o.optKey = key
	o.phase = nhPhaseOptVal
	o.input.SetValue("")
	return o, nil
}

// enterOptVal stores a custom option value, then loops back for more.
func (o *newHostWizard) enterOptVal(val string, m *Model) (overlay, tea.Cmd) {
	if val == "" {
		m.status = "value cannot be empty"
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
	default:
		return o.prompt("New host: add option", o.optKey+" value")
	}
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
