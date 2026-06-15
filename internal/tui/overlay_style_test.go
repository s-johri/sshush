package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/s-johri/sshush/pkg/knownhosts"
	"github.com/s-johri/sshush/pkg/perms"
)

// assertNoBareText fails if any non-blank line of v contains a run of 4+
// letters that is NOT inside an ANSI SGR span — i.e. text that would inherit
// the terminal's foreground color instead of the theme's (the M26 bleed).
//
// lipgloss renders a styled span as <SGR>text<reset>, where the reset is the
// empty/zero SGR (ESC[0m or ESC[m). We walk each line tracking whether we are
// currently inside such a span: an SGR that is not a bare reset opens a span,
// the reset closes it. Letters seen while no span is open are "bare".
func assertNoBareText(t *testing.T, name, v string) {
	t.Helper()
	for _, line := range strings.Split(v, "\n") {
		bare := 0
		spanDepth := 0 // >0 while inside a non-reset SGR span
		i := 0
		for i < len(line) {
			r := line[i]
			if r == '\x1b' && i+1 < len(line) && line[i+1] == '[' {
				// Parse the SGR up to its terminating 'm'.
				j := i + 2
				for j < len(line) && line[j] != 'm' {
					j++
				}
				if j < len(line) { // found 'm'
					params := line[i+2 : j]
					if params == "" || params == "0" {
						if spanDepth > 0 {
							spanDepth--
						}
					} else {
						spanDepth++
					}
					bare = 0
					i = j + 1
					continue
				}
			}
			if spanDepth == 0 && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
				bare++
				if bare >= 4 {
					t.Errorf("%s: unstyled text in line %q", name, line)
					break
				}
			} else if spanDepth == 0 {
				bare = 0
			}
			i++
		}
	}
}

// TestOverlayBodiesFullyStyled renders every overlay under a truecolor theme
// and asserts no body text leaks the terminal foreground (M26 bleed).
func TestOverlayBodiesFullyStyled(t *testing.T) {
	prevProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prevProfile)
	defer applyTheme(defaultTheme)
	drac, _ := themeByName("dracula")
	applyTheme(drac)

	m := New(&fakeService{model: snapshot(), backups: []string{"/x/config"},
		khEntries:  []knownhosts.Entry{{Hosts: []string{"github.com"}, KeyType: "ssh-ed25519", Fingerprint: "SHA256:abc", Line: 0}},
		permIssues: []perms.Issue{{Path: "/x/id_rsa", Got: 0o644, Want: 0o600, Why: "too open"}},
	})
	m = feed(m, refreshedMsg{model: snapshot()})

	host, _ := m.hostByID("web")
	cases := map[string]overlay{
		"delete-key":  &deleteConfirmOverlay{key: "id_ed"},
		"delete-host": &deleteConfirmOverlay{host: "web"},
		"edit":        newEditOverlay(host),
		"newkey":      newNewKeyWizard(),
		"newhost":     newNewHostWizard(),
		"restore":     &restoreOverlay{},
		"picker":      &pickerOverlay{host: "web"},
		"knownhosts":  &knownHostsOverlay{entries: m.svc.(*fakeService).khEntries},
		"perms":       &permsOverlay{issues: m.svc.(*fakeService).permIssues},
	}
	for name, o := range cases {
		assertNoBareText(t, name, o.View(&m))
	}
}
