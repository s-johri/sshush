package tui

// viewport tracks a cursor and scroll offset over a list of n rows shown cap at
// a time. Capacity is supplied by the caller (chrome differs per screen), so the
// viewport stays pure arithmetic — it never computes its own height. It follows
// the cursor, scrolling the minimum needed to keep it visible. See CONTEXT.md.
type viewport struct {
	cursor int
	scroll int
}

// moveCursor shifts the cursor by delta, clamps it to [0, n), and re-follows.
func (v *viewport) moveCursor(delta, n, cap int) {
	v.setCursor(v.cursor+delta, n, cap)
}

// setCursor moves the cursor to i (clamped to [0, n)) and re-follows.
func (v *viewport) setCursor(i, n, cap int) {
	v.cursor = i
	v.clampCursor(n)
	v.ensureVisible(n, cap)
}

// clampCursor keeps the cursor within [0, n) — 0 when the list is empty.
func (v *viewport) clampCursor(n int) {
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

// ensureVisible scrolls the minimum needed to keep the cursor on screen.
func (v *viewport) ensureVisible(n, cap int) {
	if n == 0 {
		v.scroll = 0
		return
	}
	if v.cursor < v.scroll {
		v.scroll = v.cursor
	}
	if v.cursor >= v.scroll+cap {
		v.scroll = v.cursor - cap + 1
	}
	if max := n - cap; v.scroll > max {
		v.scroll = max
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
}

// window returns the visible [lo, hi) row range for the current scroll offset.
func (v viewport) window(n, cap int) (int, int) {
	return clampWindow(v.scroll, n, cap)
}

// clampWindow returns the [lo, hi) slice of n rows starting near start, shown cap
// at a time, with start clamped so the window stays in range. Shared by the
// cursor-following viewport, the help overlay (scroll-only, no cursor), and the
// theme picker (which centres its cursor: start = cursor - cap/2).
func clampWindow(start, n, cap int) (int, int) {
	if start > n-cap {
		start = n - cap
	}
	if start < 0 {
		start = 0
	}
	end := start + cap
	if end > n {
		end = n
	}
	return start, end
}
