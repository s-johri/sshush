package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
)

// newKeyWizard generates a key in four phases — algorithm, bits/curve (rsa and
// ecdsa only), file name, comment — then runs ssh-keygen interactively via
// ExecProcess so it can prompt for a passphrase. One overlay, private phase
// (see CONTEXT.md).
type newKeyWizard struct {
	phase      int // 0 = algorithm, 1 = bits/curve, 2 = file name, 3 = comment
	algo       config.KeyAlgorithm
	bits       int
	algoCursor int
	bitsCursor int
	input      textinput.Model
	name       string // accepted filename, carried into the comment step
}

const (
	nkPhaseAlgo = iota
	nkPhaseBits
	nkPhaseName
	nkPhaseComment
)

// newNewKeyWizard starts the wizard at the algorithm step.
func newNewKeyWizard() *newKeyWizard {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 40
	ti.PromptStyle = textStyle
	ti.TextStyle = textStyle
	return &newKeyWizard{input: ti}
}

func (o *newKeyWizard) Update(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	if msg.String() == "esc" {
		m.status = "cancelled"
		return nil, nil
	}
	switch o.phase {
	case nkPhaseAlgo:
		return o.updateAlgo(msg)
	case nkPhaseBits:
		return o.updateBits(msg)
	case nkPhaseName:
		return o.updateName(msg, m)
	default:
		return o.updateComment(msg, m)
	}
}

// updateAlgo selects the algorithm, then advances to bits/curve selection
// (rsa/ecdsa) or straight to the filename (ed25519).
func (o *newKeyWizard) updateAlgo(msg tea.KeyMsg) (overlay, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if o.algoCursor > 0 {
			o.algoCursor--
		}
	case "down", "j":
		if o.algoCursor < len(keyAlgos)-1 {
			o.algoCursor++
		}
	case "enter":
		o.algo = keyAlgos[o.algoCursor].algo
		o.bits = 0
		if len(bitsOptions(o.algo)) > 0 {
			o.bitsCursor = 0
			o.phase = nkPhaseBits
			return o, nil
		}
		return o.toNameStep()
	}
	return o, nil
}

// updateBits selects rsa bits / ecdsa curve, then advances to the filename.
func (o *newKeyWizard) updateBits(msg tea.KeyMsg) (overlay, tea.Cmd) {
	opts := bitsOptions(o.algo)
	switch msg.String() {
	case "up", "k":
		if o.bitsCursor > 0 {
			o.bitsCursor--
		}
	case "down", "j":
		if o.bitsCursor < len(opts)-1 {
			o.bitsCursor++
		}
	case "enter":
		o.bits = opts[o.bitsCursor]
		return o.toNameStep()
	}
	return o, nil
}

// toNameStep opens the filename prompt with a sensible default name.
func (o *newKeyWizard) toNameStep() (overlay, tea.Cmd) {
	o.phase = nkPhaseName
	o.input.SetValue("id_" + string(o.algo))
	o.input.CursorEnd()
	o.input.Focus()
	return o, textinput.Blink
}

// updateName collects the key file name, then advances to the comment step.
func (o *newKeyWizard) updateName(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	if msg.String() == "enter" {
		name := strings.TrimSpace(o.input.Value())
		if name == "" {
			m.status = "key name cannot be empty"
			return o, nil
		}
		o.name = name
		o.phase = nkPhaseComment
		o.input.SetValue(name) // default comment = filename (enter accepts)
		o.input.CursorEnd()
		return o, nil
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	return o, cmd
}

// commentOrDefault is the typed comment, falling back to the filename.
func (o *newKeyWizard) commentOrDefault(name string) string {
	if c := strings.TrimSpace(o.input.Value()); c != "" {
		return c
	}
	return name
}

// updateComment collects the -C comment and runs ssh-keygen interactively (via
// ExecProcess) so it can prompt for a passphrase.
func (o *newKeyWizard) updateComment(msg tea.KeyMsg, m *Model) (overlay, tea.Cmd) {
	if msg.String() == "enter" {
		m.status = "running ssh-keygen…"
		cmd, _, err := keys.GenerateCommand(keys.GenerateOpts{
			Name: o.name, Algorithm: o.algo, Bits: o.bits, Comment: o.commentOrDefault(o.name),
		})
		if err != nil {
			m.status = "keygen error: " + err.Error()
			return nil, nil
		}
		return nil, tea.ExecProcess(cmd, func(err error) tea.Msg { return keygenDoneMsg{err: err} })
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	return o, cmd
}

// summary describes the chosen algorithm (+ bits/curve) for the filename step.
func (o *newKeyWizard) summary() string {
	if o.bits > 0 {
		unit := "bits"
		if o.algo == config.AlgECDSA {
			unit = "curve"
		}
		return fmt.Sprintf("%s, %d %s", o.algo, o.bits, unit)
	}
	return string(o.algo)
}

func (o *newKeyWizard) View(m *Model) string {
	switch o.phase {
	case nkPhaseAlgo:
		return o.viewAlgo()
	case nkPhaseBits:
		return o.viewBits()
	case nkPhaseName:
		return o.viewName()
	default:
		return o.viewComment()
	}
}

func (o *newKeyWizard) viewAlgo() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("New key: algorithm") + "\n\n")
	for i, a := range keyAlgos {
		if i == o.algoCursor {
			b.WriteString(selectedRow.Render("▸ "+a.label) + "\n")
		} else {
			b.WriteString("  " + textStyle.Render(a.label) + "\n")
		}
	}
	b.WriteString("\n" + dimStyle.Render("  ↑/↓ move · enter select · esc cancel") + "\n")
	return b.String()
}

func (o *newKeyWizard) viewBits() string {
	opts := bitsOptions(o.algo)
	label := "size (bits)"
	if o.algo == config.AlgECDSA {
		label = "curve"
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("New "+string(o.algo)+" key: "+label) + "\n\n")
	for i, n := range opts {
		line := strconv.Itoa(n)
		if i == 0 {
			line += "  (default)"
		}
		if i == o.bitsCursor {
			b.WriteString(selectedRow.Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + textStyle.Render(line) + "\n")
		}
	}
	b.WriteString("\n" + dimStyle.Render("  ↑/↓ move · enter select · esc cancel") + "\n")
	return b.String()
}

func (o *newKeyWizard) viewName() string {
	var b strings.Builder
	b.WriteString(tabActive.Render("Generate key ("+o.summary()+")") + "\n\n")
	b.WriteString(dimStyle.Render("  file name — may prompt for a passphrase") + "\n")
	b.WriteString("  " + o.input.View() + "\n\n")
	b.WriteString(dimStyle.Render("  enter confirm · esc cancel"))
	b.WriteString("\n")
	return b.String()
}

func (o *newKeyWizard) viewComment() string {
	var b strings.Builder
	b.WriteString(tabActive.Render("Generate key ("+o.summary()+")") + "\n\n")
	b.WriteString(dimStyle.Render("  comment (-C) — enter to accept the default") + "\n")
	b.WriteString("  " + o.input.View() + "\n\n")
	b.WriteString(dimStyle.Render("  enter generate · esc cancel"))
	b.WriteString("\n")
	return b.String()
}
