package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme is the full color palette. Every style is rebuilt from one of these via
// applyTheme, so the look is swappable at runtime.
type Theme struct {
	Primary lipgloss.Color // accents, titles, active tab background
	Accent  lipgloss.Color // selection text, status, flash-good
	Green   lipgloss.Color // loaded-key badge
	Dim     lipgloss.Color // muted text
	Err     lipgloss.Color // errors / destructive / flash-bad
	Gold    lipgloss.Color // default-key star, key caps
	Border  lipgloss.Color // box borders
	SelBg   lipgloss.Color // selected-row background
	Text    lipgloss.Color // body text (so it doesn't inherit the terminal fg)
	Subtle  lipgloss.Color // help labels
	HostTag lipgloss.Color // hosts-using-a-key tag
	Bg      lipgloss.Color // full-screen background ("" = terminal default)
}

// defaultTheme is the built-in palette (256-color indices), terminal background.
var defaultTheme = Theme{
	Primary: "212", Accent: "159", Green: "42", Dim: "244", Err: "203",
	Gold: "220", Border: "240", SelBg: "236", Text: "253", Subtle: "245", HostTag: "109",
}

// preset is a named theme; kept as a slice for stable switcher ordering.
type preset struct {
	name  string
	theme Theme
}

var presets = []preset{
	{"default", defaultTheme},
	{"mono", Theme{Primary: "#ffffff", Accent: "#d0d0d0", Green: "#c0c0c0", Dim: "#808080", Err: "#ff5f5f", Gold: "#e0e0e0", Border: "#5f5f5f", SelBg: "#303030", Text: "#e6e6e6", Subtle: "#9e9e9e", HostTag: "#b0b0b0", Bg: "#161616"}},
	{"high-contrast", Theme{Primary: "#ffff00", Accent: "#00ffff", Green: "#00ff00", Dim: "#c0c0c0", Err: "#ff3030", Gold: "#ffff00", Border: "#ffffff", SelBg: "#0000aa", Text: "#ffffff", Subtle: "#ffffff", HostTag: "#ff00ff", Bg: "#000000"}},
	{"dracula", Theme{Primary: "#ff79c6", Accent: "#8be9fd", Green: "#50fa7b", Dim: "#6272a4", Err: "#ff5555", Gold: "#f1fa8c", Border: "#44475a", SelBg: "#44475a", Text: "#f8f8f2", Subtle: "#6272a4", HostTag: "#bd93f9", Bg: "#282a36"}},
	{"nord", Theme{Primary: "#88c0d0", Accent: "#8fbcbb", Green: "#a3be8c", Dim: "#81a1c1", Err: "#bf616a", Gold: "#ebcb8b", Border: "#434c5e", SelBg: "#3b4252", Text: "#d8dee9", Subtle: "#81a1c1", HostTag: "#b48ead", Bg: "#2e3440"}},

	{"gruvbox-dark", Theme{Primary: "#fe8019", Accent: "#83a598", Green: "#b8bb26", Dim: "#928374", Err: "#fb4934", Gold: "#fabd2f", Border: "#504945", SelBg: "#3c3836", Text: "#ebdbb2", Subtle: "#928374", HostTag: "#d3869b", Bg: "#282828"}},
	{"gruvbox-light", Theme{Primary: "#af3a03", Accent: "#076678", Green: "#79740e", Dim: "#7c6f64", Err: "#9d0006", Gold: "#b57614", Border: "#ebdbb2", SelBg: "#ebdbb2", Text: "#3c3836", Subtle: "#928374", HostTag: "#8f3f71", Bg: "#fbf1c7"}},

	{"solarized-dark", Theme{Primary: "#268bd2", Accent: "#2aa198", Green: "#859900", Dim: "#657b83", Err: "#dc322f", Gold: "#b58900", Border: "#073642", SelBg: "#073642", Text: "#93a1a1", Subtle: "#657b83", HostTag: "#6c71c4", Bg: "#002b36"}},
	{"solarized-light", Theme{Primary: "#268bd2", Accent: "#0e8a7d", Green: "#728600", Dim: "#657b83", Err: "#dc322f", Gold: "#a07d00", Border: "#eee8d5", SelBg: "#eee8d5", Text: "#586e75", Subtle: "#657b83", HostTag: "#6c71c4", Bg: "#fdf6e3"}},

	{"catppuccin-mocha", Theme{Primary: "#f5c2e7", Accent: "#89dceb", Green: "#a6e3a1", Dim: "#6c7086", Err: "#f38ba8", Gold: "#f9e2af", Border: "#45475a", SelBg: "#313244", Text: "#cdd6f4", Subtle: "#7f849c", HostTag: "#cba6f7", Bg: "#1e1e2e"}},
	{"catppuccin-macchiato", Theme{Primary: "#f5bde6", Accent: "#8bd5ca", Green: "#a6da95", Dim: "#6e738d", Err: "#ed8796", Gold: "#eed49f", Border: "#494d64", SelBg: "#363a4f", Text: "#cad3f5", Subtle: "#8087a2", HostTag: "#c6a0f6", Bg: "#24273a"}},
	{"catppuccin-frappe", Theme{Primary: "#f4b8e4", Accent: "#99d1db", Green: "#a6d189", Dim: "#838ba7", Err: "#e78284", Gold: "#e5c890", Border: "#51576d", SelBg: "#414559", Text: "#c6d0f5", Subtle: "#838ba7", HostTag: "#ca9ee6", Bg: "#303446"}},
	{"catppuccin-latte", Theme{Primary: "#dd1c8a", Accent: "#0e7490", Green: "#2e8a1c", Dim: "#6c6f85", Err: "#d20f39", Gold: "#c25c00", Border: "#ccd0da", SelBg: "#dce0e8", Text: "#4c4f69", Subtle: "#6c6f85", HostTag: "#8839ef", Bg: "#eff1f5"}},

	{"tokyonight", Theme{Primary: "#7aa2f7", Accent: "#7dcfff", Green: "#9ece6a", Dim: "#7982a9", Err: "#f7768e", Gold: "#e0af68", Border: "#3b4261", SelBg: "#292e42", Text: "#c0caf5", Subtle: "#7982a9", HostTag: "#bb9af7", Bg: "#1a1b26"}},
	{"tokyonight-storm", Theme{Primary: "#7aa2f7", Accent: "#7dcfff", Green: "#9ece6a", Dim: "#7982a9", Err: "#f7768e", Gold: "#e0af68", Border: "#3b4261", SelBg: "#2d3149", Text: "#c0caf5", Subtle: "#7982a9", HostTag: "#bb9af7", Bg: "#24283b"}},
	{"tokyonight-day", Theme{Primary: "#2e7de9", Accent: "#007197", Green: "#587539", Dim: "#6172b0", Err: "#f52a65", Gold: "#8c6c3e", Border: "#c4c8da", SelBg: "#c4c8da", Text: "#3760bf", Subtle: "#6172b0", HostTag: "#9854f1", Bg: "#e1e2e7"}},
}

// themeByName returns a preset theme and whether it was found.
func themeByName(name string) (Theme, bool) {
	for _, p := range presets {
		if p.name == name {
			return p.theme, true
		}
	}
	return Theme{}, false
}

// applyTheme rebuilds every package style var from t. lipgloss styles are
// value types, so reassigning the vars makes the whole UI repaint on next View.
func applyTheme(t Theme) {
	colPrimary, colAccent, colGreen = t.Primary, t.Accent, t.Green
	colDim, colErr, colGold = t.Dim, t.Err, t.Gold
	colBorder, colSelBg, colBg = t.Border, t.SelBg, t.Bg

	appTitleStyle = themed(t).Bold(true).Foreground(t.Primary)
	titleStyle = themed(t).Bold(true).Foreground(t.Primary)
	tabActive = titleStyle
	// Text on a colored fill (tab/flash) picks black or white by the fill's
	// luminance, so it stays readable on both bright and dark accent colors.
	tabSelected = lipgloss.NewStyle().Bold(true).Foreground(readableOn(t.Primary)).Background(t.Primary).Padding(0, 1)
	tabUnselected = themed(t).Foreground(t.Dim).Padding(0, 1)
	headerStyle = themed(t).Foreground(t.Dim).Underline(true)
	selectedRow = lipgloss.NewStyle().Bold(true).Foreground(t.Accent).Background(t.SelBg)
	loadedBadge = themed(t).Foreground(t.Green)
	textStyle = themed(t).Foreground(t.Text)
	dimStyle = themed(t).Foreground(t.Dim)
	errStyle = themed(t).Bold(true).Foreground(t.Err)
	starStyle = themed(t).Foreground(t.Gold)
	keyCap = themed(t).Bold(true).Foreground(t.Gold)
	statusStyle = themed(t).Foreground(t.Accent)
	boxStyle = themed(t).Border(lipgloss.RoundedBorder()).BorderForeground(t.Border).Padding(0, 1)
	helpKey = themed(t).Bold(true).Foreground(t.Accent)
	helpLabel = themed(t).Bold(true).Foreground(t.Subtle)
	hostTagStyle = themed(t).Foreground(t.HostTag)
	flashGoodStyle = lipgloss.NewStyle().Bold(true).Foreground(readableOn(t.Accent)).Background(t.Accent)
	flashBadStyle = lipgloss.NewStyle().Bold(true).Foreground(readableOn(t.Err)).Background(t.Err)
}

// readableOn returns black or white — whichever reads better on fill color c
// (by Rec. 601 luminance). Non-hex colors (256-palette indices, used only by the
// bright default/mono/high-contrast fills) fall back to black.
func readableOn(c lipgloss.Color) lipgloss.Color {
	r, g, b, ok := parseHex(string(c))
	if !ok {
		return lipgloss.Color("16")
	}
	lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	if lum < 140 {
		return lipgloss.Color("#ffffff")
	}
	return lipgloss.Color("#000000")
}

// parseHex parses "#rgb" / "#rrggbb" into 8-bit components.
func parseHex(s string) (r, g, b uint8, ok bool) {
	if len(s) == 0 || s[0] != '#' {
		return 0, 0, 0, false
	}
	s = s[1:]
	switch len(s) {
	case 3:
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), true
}

// themed returns a base style carrying the theme background (so individual
// elements sit on the theme bg rather than the terminal default).
func themed(t Theme) lipgloss.Style {
	s := lipgloss.NewStyle()
	if t.Bg != "" {
		s = s.Background(t.Bg)
	}
	return s
}

const ansiReset = "\x1b[0m"

// bgOpenSeq returns the escape that sets the theme background (no reset).
func bgOpenSeq() string {
	r := lipgloss.NewStyle().Background(colBg).Render("@")
	if i := strings.IndexByte(r, '@'); i > 0 {
		return r[:i]
	}
	return ""
}

// applyBackground paints the whole screen with the theme background. Inner
// styled spans end with a full reset (\x1b[0m) which also clears the background,
// so after every reset we re-assert the bg; each line is then padded to width
// and the output is filled down to the terminal height. The result is a solid
// background instead of color only where text happens to be.
func applyBackground(s string, width, height int) string {
	if colBg == "" {
		return s
	}
	open := bgOpenSeq()
	if open == "" {
		return s // no bg escape (NO_COLOR / ascii profile) — leave output plain
	}
	lines := strings.Split(s, "\n")
	for height > 0 && len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		line = strings.ReplaceAll(line, ansiReset, ansiReset+open) // keep bg after inner resets
		if width > 0 {
			if pad := width - lipgloss.Width(line); pad > 0 {
				line += strings.Repeat(" ", pad)
			}
		}
		lines[i] = open + line + ansiReset
	}
	return strings.Join(lines, "\n")
}

func init() { applyTheme(defaultTheme) }
