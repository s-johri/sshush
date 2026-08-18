package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// Deterministic output for the width and offset assertions.
//
// lipgloss v1 had a global color profile, and a test made output countable
// with lipgloss.SetColorProfile(termenv.Ascii). v2 removed the renderer and the
// global profile: Style.Render always emits full-fidelity ANSI, and the output
// layer reduces it. Under bubbletea that layer is the renderer, which the tests
// do not go through, so a test that wants plain text must reduce the string
// itself. These helpers are that reduction, kept in one place.

// downsample reduces the escape sequences in s to what profile p supports. It
// is the same reduction that bubbletea applies to a view before it reaches the
// terminal.
func downsample(p colorprofile.Profile, s string) string {
	var b strings.Builder
	w := &colorprofile.Writer{Forward: &b, Profile: p}
	if _, err := w.WriteString(s); err != nil {
		panic(err) // strings.Builder never fails
	}
	return b.String()
}

// plain strips all styling from s, which is what the v1 termenv.Ascii profile
// did. colorprofile.Ascii is not the equivalent: it drops color but keeps bold
// and underline. colorprofile.NoTTY is the one that drops everything.
func plain(s string) string { return downsample(colorprofile.NoTTY, s) }

// view renders m the way a terminal finally shows it, with the styling reduced
// away. Tests use this rather than render: under v1 the global renderer
// detected that the test stdout was not a terminal and made every Render call
// emit plain text, so assertions could count columns and match substrings for
// free. v2 has no such detection at render time.
func view(m Model) string { return plain(m.render()) }

// TestPlainStripsStyling guards the helper itself: the offset assertions in the
// other tests are only meaningful while plain really does remove every escape.
func TestPlainStripsStyling(t *testing.T) {
	styled := titleStyle.Render("sshush")
	if !strings.ContainsRune(styled, '\x1b') {
		t.Fatal("setup: titleStyle rendered no escapes, nothing to strip")
	}
	got := plain(styled)
	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("plain left escapes: %q", got)
	}
	if got != "sshush" {
		t.Errorf("plain changed the text: %q, want %q", got, "sshush")
	}
}
