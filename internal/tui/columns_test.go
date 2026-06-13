package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/s-johri/sshush/pkg/config"
)

// col returns the visual column where sub starts in line, or -1 if absent.
// strings.Index gives byte offsets, but the panes use multi-byte glyphs
// (│ ★ ↪ ○ ▸), so column alignment must be measured in display width.
func col(line, sub string) int {
	b := strings.Index(line, sub)
	if b < 0 {
		return -1
	}
	return lipgloss.Width(line[:b])
}

func colSnap() *config.SshConfigModel {
	return &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"id_def":   {ID: "id_def", Name: "id_def", Algorithm: config.AlgED25519, ExistsOnDisk: true, Comment: "work laptop"},
			"id_plain": {ID: "id_plain", Name: "id_plain", Algorithm: config.AlgRSA, ExistsOnDisk: true, Comment: "legacy"},
		},
		Hosts: map[config.HostID]config.Host{
			"web": {ID: "web", Name: "web", Hostname: "1.2.3.4", User: "u",
				Identities: []config.IdentityID{"id_def", "id_plain"}},
		},
	}
}

// TestKeysPaneColumnsAligned: with one default and one non-default key, the
// name/algo/hosts/comment columns start at identical offsets in the header and
// in every row — the original bug was fields shifting when ★ was absent.
func TestKeysPaneColumnsAligned(t *testing.T) {
	prev := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(prev)
	lipgloss.SetColorProfile(termenv.Ascii) // plain text: offsets are countable
	fs := &fakeSettings{defaults: []config.IdentityID{"id_def"}}
	m := New(&fakeService{model: colSnap()}).WithSettings(fs)
	m = feed(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = feed(m, refreshedMsg{model: colSnap()})

	v := m.View()
	var header, defRow, plainRow string
	for _, ln := range strings.Split(v, "\n") {
		switch {
		case strings.Contains(ln, "name") && strings.Contains(ln, "algo"):
			header = ln
		case strings.Contains(ln, "id_def "):
			defRow = ln
		case strings.Contains(ln, "id_plain"):
			plainRow = ln
		}
	}
	if header == "" || defRow == "" || plainRow == "" {
		t.Fatalf("missing header/rows in:\n%s", v)
	}
	if !strings.Contains(defRow, "★") {
		t.Fatalf("default key should show ★: %q", defRow)
	}
	if strings.Contains(plainRow, "★") {
		t.Fatalf("non-default key must not show ★: %q", plainRow)
	}
	// Column starts must match across header and both rows.
	for _, probe := range []struct{ label, hdr, inDef, inPlain string }{
		{"name", "name", "id_def", "id_plain"},
		{"algo", "algo", "ed25519", "rsa"},
		{"hosts", "hosts", "↪ web", "↪ web"},
		{"comment", "comment", "work laptop", "legacy"},
	} {
		h := col(header, probe.hdr)
		d := col(defRow, probe.inDef)
		p := col(plainRow, probe.inPlain)
		if h < 0 || d < 0 || p < 0 || h != d || d != p {
			t.Errorf("%s column misaligned: header=%d def=%d plain=%d\nH:%q\nD:%q\nP:%q",
				probe.label, h, d, p, header, defRow, plainRow)
		}
	}
}

// TestHostsPaneHeaderAligned: the hosts header columns line up with row content.
func TestHostsPaneHeaderAligned(t *testing.T) {
	prev := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(prev)
	lipgloss.SetColorProfile(termenv.Ascii)
	m := New(&fakeService{model: colSnap()})
	m = feed(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = feed(m, refreshedMsg{model: colSnap()})
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab})

	v := m.View()
	var header, row string
	for _, ln := range strings.Split(v, "\n") {
		switch {
		case strings.Contains(ln, "host") && strings.Contains(ln, "destination"):
			header = ln
		case strings.Contains(ln, "u@1.2.3.4"):
			row = ln
		}
	}
	if header == "" || row == "" {
		t.Fatalf("missing header/row in:\n%s", v)
	}
	if h, r := col(header, "destination"), col(row, "u@1.2.3.4"); h != r {
		t.Errorf("destination column misaligned: header=%d row=%d\nH:%q\nR:%q", h, r, header, row)
	}
	if h, r := col(header, "host"), col(row, "web"); h != r {
		t.Errorf("host column misaligned: header=%d row=%d", h, r)
	}
}
