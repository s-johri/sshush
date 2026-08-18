package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestViewIsPadded(t *testing.T) {

	m := New(&fakeService{model: snapshot()})
	m = feed(m, tea.WindowSizeMsg{Width: 60, Height: 20})
	m = feed(m, refreshedMsg{model: snapshot()})

	lines := strings.Split(view(m), "\n")
	if strings.TrimSpace(lines[0]) != "" {
		t.Errorf("first line should be blank padding, got %q", lines[0])
	}
	// First content line starts after 2 columns of padding.
	if !strings.HasPrefix(lines[1], "  sshush") {
		t.Errorf("content not inset by 2 cols: %q", lines[1])
	}
	if len(lines) > 20 {
		t.Errorf("padded view overflows height: %d lines", len(lines))
	}
}

func TestPaddingDroppedWhenNarrow(t *testing.T) {

	m := New(&fakeService{model: snapshot()})
	m = feed(m, tea.WindowSizeMsg{Width: 36, Height: 10})
	m = feed(m, refreshedMsg{model: snapshot()})

	lines := strings.Split(view(m), "\n")
	if strings.HasPrefix(lines[0], "  ") || strings.TrimSpace(lines[0]) == "" {
		t.Errorf("narrow terminal must not be padded, first line %q", lines[0])
	}
	if len(lines) > 10 {
		t.Errorf("view overflows narrow height: %d lines", len(lines))
	}
}
