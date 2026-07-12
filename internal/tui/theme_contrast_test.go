package tui

import (
	"math"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// relLuminance is the WCAG relative luminance of a hex color (0 black..1 white).
func relLuminance(c lipgloss.Color) (float64, bool) {
	r, g, b, ok := parseHex(string(c))
	if !ok {
		return 0, false // 256-color index (default theme): not auditable
	}
	lin := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b), true
}

// contrastRatio is the WCAG contrast ratio between two hex colors (1..21).
func contrastRatio(a, b lipgloss.Color) (float64, bool) {
	la, ok1 := relLuminance(a)
	lb, ok2 := relLuminance(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	hi, lo := math.Max(la, lb), math.Min(la, lb)
	return (hi + 0.05) / (lo + 0.05), true
}

// TestThemeContrast is the readability gate: every preset's roles must clear a
// minimum contrast against its background. Bg-less presets (the 256-color
// default) are skipped. This is a permanent regression gate for new themes.
func TestThemeContrast(t *testing.T) {
	for _, p := range presets {
		if p.theme.Bg == "" {
			continue
		}
		checks := []struct {
			role string
			fg   lipgloss.Color
			bg   lipgloss.Color
			min  float64
		}{
			{"Text", p.theme.Text, p.theme.Bg, 4.5},
			{"Dim", p.theme.Dim, p.theme.Bg, 3.0},
			{"Subtle", p.theme.Subtle, p.theme.Bg, 3.0},
			{"Primary", p.theme.Primary, p.theme.Bg, 3.0},
			{"Accent", p.theme.Accent, p.theme.Bg, 3.0},
			{"Err", p.theme.Err, p.theme.Bg, 3.0},
			{"Gold", p.theme.Gold, p.theme.Bg, 3.0},
			{"Green", p.theme.Green, p.theme.Bg, 3.0},
			{"HostTag", p.theme.HostTag, p.theme.Bg, 3.0},
			{"Accent/SelBg", p.theme.Accent, p.theme.SelBg, 3.0},
			// Borders are decoration, not text: they only need to be
			// discernible, so the floor is well below the text minimums.
			{"Border", p.theme.Border, p.theme.Bg, 1.3},
		}
		for _, c := range checks {
			ratio, ok := contrastRatio(c.fg, c.bg)
			if !ok {
				continue
			}
			if ratio < c.min {
				t.Errorf("%s: %s %s on %s = %.2f:1, want >= %.1f:1",
					p.name, c.role, c.fg, c.bg, ratio, c.min)
			}
		}
	}
}
