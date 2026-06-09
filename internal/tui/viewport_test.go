package tui

import "testing"

func TestViewportFollowsCursorDown(t *testing.T) {
	v := viewport{}
	// 30 rows, 10 visible. Walk to the bottom one step at a time.
	for i := 0; i < 40; i++ {
		v.moveCursor(1, 30, 10)
	}
	if v.cursor != 29 {
		t.Fatalf("cursor = %d, want 29 (clamped to last)", v.cursor)
	}
	lo, hi := v.window(30, 10)
	if v.cursor < lo || v.cursor >= hi {
		t.Errorf("cursor %d not in window [%d,%d)", v.cursor, lo, hi)
	}
	if hi != 30 {
		t.Errorf("window should reach the end: hi=%d", hi)
	}
}

func TestViewportClampsCursorTop(t *testing.T) {
	v := viewport{cursor: 5, scroll: 5}
	v.moveCursor(-20, 30, 10)
	if v.cursor != 0 || v.scroll != 0 {
		t.Errorf("cursor=%d scroll=%d, want 0/0", v.cursor, v.scroll)
	}
}

func TestViewportSetCursorScrollsIntoView(t *testing.T) {
	v := viewport{}
	v.setCursor(25, 30, 10) // jump near the bottom
	if v.cursor != 25 {
		t.Fatalf("cursor = %d, want 25", v.cursor)
	}
	lo, hi := v.window(30, 10)
	if 25 < lo || 25 >= hi {
		t.Errorf("cursor 25 not visible in [%d,%d)", lo, hi)
	}
}

func TestViewportEnsureVisibleAfterShrink(t *testing.T) {
	// Cursor was deep in a long list; the list (or capacity) shrinks under it.
	v := viewport{cursor: 25, scroll: 16}
	v.clampCursor(5) // list shrank to 5 rows
	v.ensureVisible(5, 10)
	if v.cursor != 4 {
		t.Errorf("cursor = %d, want 4", v.cursor)
	}
	if v.scroll != 0 {
		t.Errorf("scroll = %d, want 0 (everything fits)", v.scroll)
	}
}

func TestViewportEmptyList(t *testing.T) {
	v := viewport{cursor: 3, scroll: 2}
	v.clampCursor(0)
	v.ensureVisible(0, 10)
	if v.cursor != 0 || v.scroll != 0 {
		t.Errorf("empty list: cursor=%d scroll=%d, want 0/0", v.cursor, v.scroll)
	}
	lo, hi := v.window(0, 10)
	if lo != 0 || hi != 0 {
		t.Errorf("empty window = [%d,%d), want [0,0)", lo, hi)
	}
}

func TestClampWindow(t *testing.T) {
	cases := []struct {
		name           string
		start, n, cap  int
		wantLo, wantHi int
	}{
		{"top", 0, 30, 10, 0, 10},
		{"middle", 12, 30, 10, 12, 22},
		{"past end clamps back", 25, 30, 10, 20, 30},
		{"negative start", -4, 30, 10, 0, 10},
		{"cap exceeds n", 0, 5, 10, 0, 5},
		{"huge cap sentinel", 0, 7, 1 << 30, 0, 7},
	}
	for _, c := range cases {
		lo, hi := clampWindow(c.start, c.n, c.cap)
		if lo != c.wantLo || hi != c.wantHi {
			t.Errorf("%s: clampWindow(%d,%d,%d) = [%d,%d), want [%d,%d)",
				c.name, c.start, c.n, c.cap, lo, hi, c.wantLo, c.wantHi)
		}
	}
}

// The theme picker centres its cursor: start = cursor - cap/2, then clamp.
func TestClampWindowCentred(t *testing.T) {
	// cursor 8 of 16 presets, capacity 6 → centred window around 8.
	lo, hi := clampWindow(8-6/2, 16, 6)
	if lo != 5 || hi != 11 {
		t.Errorf("centred window = [%d,%d), want [5,11)", lo, hi)
	}
	// Near the end it clamps to the last full page rather than centring.
	lo, hi = clampWindow(15-6/2, 16, 6)
	if hi != 16 || lo != 10 {
		t.Errorf("centred-at-end = [%d,%d), want [10,16)", lo, hi)
	}
}
