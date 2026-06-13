# Soak Polish (v0.9.2 / v0.9.3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix seven soak findings: theme readability (contrast gate + fg-bleed), pane column alignment, app padding, connect/copy correctness with custom configs (→ v0.9.2); keygen comment step and a `sshush install-extras` subcommand (→ v0.9.3).

**Architecture:** All TUI work happens in `internal/tui` (lipgloss styles in `theme.go`, panes in `tui.go`, modal screens as `overlay_*.go` adapters behind the `overlay` seam). The CLI lives in `cmd/sshush`. Spec: `docs/superpowers/specs/2026-06-12-soak-polish-design.md`.

**Tech Stack:** Go 1.25, bubbletea v1, lipgloss v1, `go:embed`, goreleaser.

**Conventions for every task:** run `gofmt -w` on touched files before committing; `go build ./... && go vet ./... && go test ./...` must pass at every commit. Tests live in the same package (`package tui` / `package main`). The tui test helpers `feed(m, msg)`, `key("x")`, `snapshot()`, `fakeService`, `fakeSettings` already exist in `internal/tui/tui_test.go`. Tests that change the theme MUST `defer applyTheme(defaultTheme)` (see ADR 0001).

---

## Batch A → v0.9.2

### Task 1: Contrast gate + palette fixes

**Files:**
- Create: `internal/tui/theme_contrast_test.go`
- Modify: `internal/tui/theme.go` (palette values that fail the gate)

- [ ] **Step 1: Write the gate test**

`parseHex` already exists in `theme.go`. Create `internal/tui/theme_contrast_test.go`:

```go
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
```

- [ ] **Step 2: Run it — expect failures**

Run: `go test ./internal/tui/ -run TestThemeContrast -v`
Expected: FAIL listing offenders. Known in advance: `nord Dim #4c566a` (~1.8:1), `nord Subtle #616e88` (~2.5:1), `tokyonight`/`tokyonight-storm` `Dim`/`Subtle #565f89` (~2.8:1), `solarized-dark Dim #586e75` (~2.8:1). The full list comes from the test output.

- [ ] **Step 3: Fix the failing palette values in `theme.go`**

Starting replacements (same upstream palette, next readable shade) — then iterate on whatever the gate still reports until it passes:

| theme | role | old | new |
|---|---|---|---|
| nord | Dim | `#4c566a` | `#81a1c1` (frost) |
| nord | Subtle | `#616e88` | `#81a1c1` |
| tokyonight / -storm | Dim, Subtle | `#565f89` | `#7982a9` |
| solarized-dark | Dim | `#586e75` | `#657b83` (base00) |

Edit the corresponding entries in the `presets` slice in `internal/tui/theme.go`. If other themes fail, lighten (dark bg) or darken (light bg) the reported hex toward the theme's `Text` value until the ratio clears; prefer documented palette shades when one exists.

- [ ] **Step 4: Run the gate until green, then full suite**

Run: `go test ./internal/tui/ -run TestThemeContrast -v` → PASS
Run: `go build ./... && go vet ./... && go test ./...` → PASS
(Existing theme render tests assert behavior, not exact hex values, so palette changes don't break them; if one does, update its expected color.)

- [ ] **Step 5: Commit**

```bash
git add internal/tui/theme.go internal/tui/theme_contrast_test.go
git commit -m "Add theme contrast gate and fix low-contrast palette values"
```

---

### Task 2: Foreground-bleed fix (style every overlay body string)

**Files:**
- Modify: `internal/tui/overlay_delete.go`, `overlay_edit.go`, `overlay_newkey.go`, `overlay_newhost.go`, `overlay_knownhosts.go`, `overlay_picker.go`, `overlay_restore.go`
- Test: `internal/tui/overlay_style_test.go` (create)

Rule: every rendered string goes through a theme style — `textStyle` for body text, `dimStyle` for secondary. Nothing may inherit the terminal foreground.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/overlay_style_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/knownhosts"
	"github.com/s-johri/sshush/pkg/perms"
)

// assertNoBareText fails if any non-blank line of v contains a run of 4+
// letters outside an ANSI SGR span — i.e. text that would inherit the
// terminal's foreground color instead of the theme's.
func assertNoBareText(t *testing.T, name, v string) {
	t.Helper()
	for _, line := range strings.Split(v, "\n") {
		bare := 0
		inEsc := false
		styled := false // inside an SGR span
		for _, r := range line {
			switch {
			case r == '\x1b':
				inEsc = true
			case inEsc:
				if r == 'm' {
					inEsc = false
					styled = true // entered (or left) a span; reset detection below
				}
			case r >= 'A' && r <= 'z' && !styled:
				bare++
				if bare >= 4 {
					t.Errorf("%s: unstyled text in line %q", name, line)
					return
				}
			default:
				if !styled {
					bare = 0
				}
			}
		}
	}
}

// TestOverlayBodiesFullyStyled renders every overlay under a truecolor theme
// and asserts no body text leaks the terminal foreground (M26 bleed).
func TestOverlayBodiesFullyStyled(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer applyTheme(defaultTheme)
	drac, _ := themeByName("dracula")
	applyTheme(drac)

	m := New(&fakeService{model: snapshot(), backups: []string{"/x/config"},
		khEntries: []knownhosts.Entry{{Hosts: []string{"github.com"}, KeyType: "ssh-ed25519", Fingerprint: "SHA256:abc", Line: 0}},
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
```

Note: `New(...)` takes the service; `snapshot()` has host `web`. If `assertNoBareText` proves too strict/loose when run, tune only the detector — the assertion intent (no unstyled runs) stays.

- [ ] **Step 2: Run it — expect failures**

Run: `go test ./internal/tui/ -run TestOverlayBodiesFullyStyled -v`
Expected: FAIL for delete/edit/newkey/newhost/restore/picker/knownhosts overlays (each has bare `b.WriteString("  ...")` lines).

- [ ] **Step 3: Style every bare string**

Apply in each file — wrap the *text* (not the leading spacing, which is fine either way):

`overlay_delete.go`:
```go
b.WriteString(textStyle.Render(fmt.Sprintf("  Permanently delete %s and its .pub from disk", o.key)) + "\n")
// and
b.WriteString(textStyle.Render(fmt.Sprintf("  Remove host %s from the config", o.host)) + "\n")
```

`overlay_edit.go` (viewValue, viewOptName, viewConfirm):
```go
b.WriteString(textStyle.Render("  "+o.newKey) + "\n")
b.WriteString(textStyle.Render("  "+o.activeField()) + "\n")
b.WriteString(dimStyle.Render("  option name (e.g. ForwardAgent)") + "\n")
b.WriteString(textStyle.Render(fmt.Sprintf("  Remove %s from %s", field, o.host)) + "\n")
b.WriteString(textStyle.Render(fmt.Sprintf("  Set %s of %s to %q", field, o.host, val)) + "\n")
```

`overlay_newkey.go` (viewAlgo non-cursor rows, viewBits non-cursor rows, viewName hint):
```go
b.WriteString("  " + textStyle.Render(a.label) + "\n")
b.WriteString("  " + textStyle.Render(line) + "\n")
b.WriteString(dimStyle.Render("  file name — may prompt for a passphrase") + "\n")
```

`overlay_newhost.go` (prompt hint):
```go
b.WriteString(dimStyle.Render("  "+hint) + "\n")
```

`overlay_knownhosts.go` (non-cursor row — wrap host+keytype; fingerprint already dim):
```go
line := textStyle.Render(fmt.Sprintf("%-28s %-20s ", host, e.KeyType)) + dimStyle.Render(e.Fingerprint)
```
(note: when `e.Hashed`, `host` is already a dim-styled string — move the `Hashed` dimming *after* building the plain `%-28s` cell so padding counts runes, not ANSI: pad first, then style.)

`overlay_picker.go` (non-cursor row):
```go
b.WriteString("  " + glyphStyle.Render(glyph) + " " + textStyle.Render(id.Name) + "\n")
```

`overlay_restore.go` (explanation lines):
```go
b.WriteString(textStyle.Render("  Revert these file(s) to their .bak snapshot (taken before") + "\n")
b.WriteString(textStyle.Render("  sshush's first edit), discarding changes made since:") + "\n")
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ -run 'TestOverlayBodiesFullyStyled' -v` → PASS
Run: `go test ./internal/tui/` → PASS (some existing tests assert on plain substrings — `strings.Contains` still matches because ANSI wraps, not rewrites, the text).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/overlay_*.go internal/tui/overlay_style_test.go
git commit -m "Route all overlay body text through theme styles"
```

---

### Task 3: Column engine for keys/hosts panes

**Files:**
- Modify: `internal/tui/tui.go` (`keysLines`, `hostsLines`, header construction; `listRow` unchanged)
- Test: `internal/tui/columns_test.go` (create)

Layout (chosen option B): row = 2-col `listRow` prefix + `● `/`○ ` + `★ `/`  ` + `name`(20) + ` ` + `algo`(11) + ` ` + `hosts`(flex hostsW) + `  ` + `comment`(dim remainder). The header is built from the same widths so it cannot drift. `hostsW` = widest hosts tag among visible rows, clamped to [5, 28].

- [ ] **Step 1: Write the failing alignment test**

Create `internal/tui/columns_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/s-johri/sshush/pkg/config"
)

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
		h := strings.Index(header, probe.hdr)
		d := strings.Index(defRow, probe.inDef)
		p := strings.Index(plainRow, probe.inPlain)
		if h < 0 || d < 0 || p < 0 || h != d || d != p {
			t.Errorf("%s column misaligned: header=%d def=%d plain=%d\nH:%q\nD:%q\nP:%q",
				probe.label, h, d, p, header, defRow, plainRow)
		}
	}
}

// TestHostsPaneHeaderAligned: the hosts header columns line up with row content.
func TestHostsPaneHeaderAligned(t *testing.T) {
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
	if h, r := strings.Index(header, "destination"), strings.Index(row, "u@1.2.3.4"); h != r {
		t.Errorf("destination column misaligned: header=%d row=%d\nH:%q\nR:%q", h, r, header, row)
	}
	if h, r := strings.Index(header, "host"), strings.Index(row, "web"); h != r {
		t.Errorf("host column misaligned: header=%d row=%d", h, r)
	}
}
```

Check `fakeSettings` field name for defaults in `tui_test.go` (it stores the toggled list — if the field is named differently, e.g. `defaults` vs `defaultIDs`, match it; `IsDefault` is the method the pane calls).

- [ ] **Step 2: Run — expect alignment failures**

Run: `go test ./internal/tui/ -run 'ColumnsAligned|HeaderAligned' -v`
Expected: FAIL (header built with different offsets than rows; ★ shifts fields).

- [ ] **Step 3: Rewrite `keysLines` and `hostsLines` on the shared widths**

In `internal/tui/tui.go`, replace the body of `keysLines` (keep signature):

```go
func (m Model) keysLines(w int) []string {
	vis := m.visibleIDs()
	if len(m.ids) == 0 {
		return []string{dimStyle.Render("no keys found")}
	}
	if len(vis) == 0 {
		return []string{dimStyle.Render("no keys match " + strconv.Quote(m.filterQuery()))}
	}
	usedBy := m.hostsByKey()
	start, end := m.window(paneKeys)

	// hosts column width: widest visible tag, clamped to [5, 28].
	hostsW := 5
	for i := start; i < end; i++ {
		if hosts := usedBy[vis[i].ID]; len(hosts) > 0 {
			if l := lipgloss.Width("↪ " + strings.Join(hosts, ", ")); l > hostsW {
				hostsW = l
			}
		}
	}
	if hostsW > 28 {
		hostsW = 28
	}

	// Header from the same widths as the rows: listRow prefix (2) + gutter
	// "● ★ " (4) + name(20)+1 + algo(11)+1 + hosts(hostsW)+2 + comment.
	header := strings.Repeat(" ", 2+4) +
		padClip("name", 20) + " " + padClip("algo", 11) + " " +
		padClip("hosts", hostsW) + "  comment"
	lines := []string{fit(headerStyle.Render(header), w)}

	for i := start; i < end; i++ {
		id := vis[i]
		glyph, glyphStyle := glyphUnloaded, dimStyle
		if id.LoadedInAgent {
			glyph, glyphStyle = glyphLoaded, loadedBadge
		}
		star, starStyled := " ", " "
		if m.settings != nil && m.settings.IsDefault(id.ID) {
			star, starStyled = "★", starStyle.Render("★")
		}
		algo := string(id.Algorithm)
		if !id.ExistsOnDisk {
			algo = "agent-only"
		}
		tag := ""
		if hosts := usedBy[id.ID]; len(hosts) > 0 {
			tag = "↪ " + strings.Join(hosts, ", ")
		}

		nameCol, algoCol, hostsCol := padClip(id.Name, 20), padClip(algo, 11), padClip(tag, hostsW)
		plain := glyph + " " + star + " " + nameCol + " " + algoCol + " " + hostsCol + "  " + id.Comment
		styled := glyphStyle.Render(glyph) + " " + starStyled + " " +
			textStyle.Render(nameCol) + " " + dimStyle.Render(algoCol) + " " +
			hostTagStyle.Render(hostsCol) + "  " + dimStyle.Render(id.Comment)
		lines = append(lines, m.listRow(paneKeys, i, plain, styled, w))
	}
	if ind := m.scrollIndicator(paneKeys); ind != "" {
		lines = append(lines, fit(ind, w))
	}
	return lines
}
```

In `hostsLines`, only the header changes (rows already use `padClip(...,20)`):

```go
	lines := []string{fit(headerStyle.Render(strings.Repeat(" ", 2)+padClip("host", 20)+" destination"), w)}
```

Comment truncation priority is preserved structurally: comment is last, so `fit`/`listRow` width-clipping drops it first; hosts is `padClip`-ed (its own `…`).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ -run 'ColumnsAligned|HeaderAligned' -v` → PASS
Run: `go test ./internal/tui/` → fix any render-assert tests that matched the old `★ default` long form (the new form is a bare `★` in the gutter; update expectations — e.g. the demo/readme strings are not tests, but `TestSnapshotSortedAndRendered`-style assertions may match on `●` only and stay green).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tui.go internal/tui/columns_test.go internal/tui/tui_test.go
git commit -m "Render pane headers and rows through shared fixed columns"
```

---

### Task 4: App padding

**Files:**
- Modify: `internal/tui/tui.go` (`View`, new `padding`/`applyPadding`, `listCapacity`, `boxInner`, `box`)
- Test: `internal/tui/padding_test.go` (create)

Design: compute `padX, padY` (2,1 — or 0,0 when width < 40 or height < 12). `View` renders `viewInner` on a copy of the model with `width/height` reduced by the padding, pads the result, then background-fills at full size. Inner code (capacities, boxes, overlays, flash bar) needs no knowledge of padding.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/padding_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestViewIsPadded(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := New(&fakeService{model: snapshot()})
	m = feed(m, tea.WindowSizeMsg{Width: 60, Height: 20})
	m = feed(m, refreshedMsg{model: snapshot()})

	lines := strings.Split(m.View(), "\n")
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
	lipgloss.SetColorProfile(termenv.Ascii)
	m := New(&fakeService{model: snapshot()})
	m = feed(m, tea.WindowSizeMsg{Width: 36, Height: 10})
	m = feed(m, refreshedMsg{model: snapshot()})

	lines := strings.Split(m.View(), "\n")
	if strings.HasPrefix(lines[0], "  ") || strings.TrimSpace(lines[0]) == "" {
		t.Errorf("narrow terminal must not be padded, first line %q", lines[0])
	}
	if len(lines) > 10 {
		t.Errorf("view overflows narrow height: %d lines", len(lines))
	}
}
```

- [ ] **Step 2: Run — expect failure**

Run: `go test ./internal/tui/ -run 'TestViewIsPadded|TestPaddingDropped' -v`
Expected: `TestViewIsPadded` FAILs (no blank first line / no inset).

- [ ] **Step 3: Implement**

In `internal/tui/tui.go` replace `View` and add helpers:

```go
// padding is the breathing room around the whole app: 1 row top/bottom and
// 2 cols left/right, dropped entirely on small terminals so content wins.
func (m Model) padding() (x, y int) {
	if m.width >= 40 && m.height >= 12 {
		return 2, 1
	}
	return 0, 0
}

// applyPadding insets s by x columns and y blank rows (no trailing newline).
func applyPadding(s string, x, y int) string {
	lines := strings.Split(s, "\n")
	pad := strings.Repeat(" ", x)
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	blank := make([]string, y)
	out := append(append(blank, lines...), make([]string, y)...)
	return strings.Join(out, "\n")
}

// View renders the current screen, then layers padding, the theme background,
// and any active screen-shake over the whole output.
func (m Model) View() string {
	padX, padY := m.padding()
	inner := m
	inner.width -= 2 * padX
	inner.height -= 2 * padY
	out := inner.viewInner()
	if padX > 0 || padY > 0 {
		out = applyPadding(out, padX, padY)
	}
	out = applyBackground(out, m.width, m.height)
	if m.fxActive() && m.fx.shakeAmp > 0 {
		out = m.applyShake(out)
	}
	return out
}
```

`viewInner`, `listCapacity`, `boxInner`, `box`, and every overlay capacity already read `m.width`/`m.height` from the receiver — the reduced copy feeds them automatically (`viewInner` passes `&inner` to `m.modal.View` because it is a method on the copy). No other changes.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ -run 'TestViewIsPadded|TestPaddingDropped' -v` → PASS
Run: `go test ./internal/tui/` → some render tests assert on first-line content or exact widths at sizes ≥40×12; update them for the 2-col/1-row inset (or use width 36 to opt out of padding where the test's subject is unrelated to padding). `TestBackgroundFillsWholeScreen` (pure `applyBackground`) is unaffected.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tui.go internal/tui/padding_test.go internal/tui/tui_test.go
git commit -m "Add app padding inside the themed background fill"
```

---

### Task 5: Connect via -F; explicit copy command

**Files:**
- Modify: `internal/tui/tui.go` (`connectToHost`, `sshCommand` → `sshCommandFor`, `beginCopy`, new Model field + option), `cmd/sshush/main.go` (plumb config path)
- Test: append to `internal/tui/tui_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/tui_test.go`:

```go
func TestSSHArgsUseConfigFlagOnlyWhenCustom(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	if got := m.sshArgs("web"); !slicesEqual(got, []string{"web"}) {
		t.Errorf("default config: args = %v, want [web]", got)
	}
	m = m.WithConfigFlag("/work/sshcfg")
	if got := m.sshArgs("web"); !slicesEqual(got, []string{"-F", "/work/sshcfg", "web"}) {
		t.Errorf("custom config: args = %v", got)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSSHCommandForExplicitExpansion(t *testing.T) {
	snap := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{
			"id_a": {ID: "id_a", Name: "id_a", Path: "/k/id_a", ExistsOnDisk: true},
		},
		Hosts: map[config.HostID]config.Host{
			"db": {ID: "db", Name: "db", Hostname: "10.0.0.5", User: "postgres", Port: 2222,
				Identities: []config.IdentityID{"id_a"},
				Options:    map[string]string{"ProxyJump": "bastion", "ForwardAgent": "yes"}},
		},
	}
	m := New(&fakeService{model: snap})
	m = feed(m, refreshedMsg{model: snap})

	h, _ := m.hostByID("db")
	got := m.sshCommandFor(h)
	want := "ssh -p 2222 -i /k/id_a -o ForwardAgent=yes -o ProxyJump=bastion postgres@10.0.0.5"
	if got != want {
		t.Errorf("explicit command:\n got %q\nwant %q", got, want)
	}
}

func TestSSHCommandForNoHostnameFallsBack(t *testing.T) {
	m := New(&fakeService{model: snapshot()}).WithConfigFlag("/work/sshcfg")
	m = feed(m, refreshedMsg{model: snapshot()})
	h := config.Host{ID: "bare", Name: "bare"}
	if got := m.sshCommandFor(h); got != "ssh -F /work/sshcfg bare" {
		t.Errorf("fallback = %q", got)
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

Run: `go test ./internal/tui/ -run 'TestSSHArgs|TestSSHCommandFor' -v`
Expected: FAIL — `sshArgs`, `WithConfigFlag`, `sshCommandFor` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/tui.go`:

Add to the Model struct (near `sshDir`):
```go
	// cfgFlag is the custom SSH config path to pass to ssh as -F. Empty means
	// the user runs on the default config — never pass -F then, because -F
	// also suppresses /etc/ssh/ssh_config.
	cfgFlag string
```

Add the option (near the other With*):
```go
// WithConfigFlag makes connect/copy commands carry `-F path` so ssh resolves
// hosts against the custom config sshush is managing. Only set when the user
// configured a non-default config location.
func (m Model) WithConfigFlag(path string) Model {
	m.cfgFlag = path
	return m
}

// sshArgs builds the argv (after "ssh") for connecting to alias.
func (m Model) sshArgs(alias string) []string {
	if m.cfgFlag != "" {
		return []string{"-F", m.cfgFlag, alias}
	}
	return []string{alias}
}
```

In `connectToHost`, replace `cmd := exec.Command("ssh", alias)` with:
```go
	cmd := exec.Command("ssh", m.sshArgs(alias)...)
```

Replace `sshCommand` with a Model method (delete the old func; update the `beginCopy` call site to `m.sshCommandFor(sel)`):
```go
// sshCommandFor builds the shareable, explicit ssh invocation for a host from
// its own config block: port, identities, and options expanded as flags. It
// intentionally reflects only this block — wildcard/Match merging is ssh's job
// at connect time. Hosts with no HostName fall back to the alias form (with
// -F when a custom config is set), since there is nothing to expand.
func (m Model) sshCommandFor(h config.Host) string {
	if h.Hostname == "" {
		return "ssh " + strings.Join(m.sshArgs(firstAlias(h.Name)), " ")
	}
	cmd := "ssh"
	if h.Port != 0 {
		cmd += fmt.Sprintf(" -p %d", h.Port)
	}
	for _, id := range h.Identities {
		for _, ident := range m.ids {
			if ident.ID == id && ident.Path != "" {
				cmd += " -i " + ident.Path
				break
			}
		}
	}
	for _, k := range sortedOptionKeys(h.Options) {
		cmd += fmt.Sprintf(" -o %s=%s", k, h.Options[k])
	}
	if h.User != "" {
		return cmd + " " + h.User + "@" + h.Hostname
	}
	return cmd + " " + h.Hostname
}

// sortedOptionKeys returns a host's option names in sorted order (determinism).
func sortedOptionKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
```

In `cmd/sshush/main.go` `runTUI`, after `model = model.WithSettings(...)`:
```go
	// A custom config means `ssh <alias>` would resolve against the wrong
	// file; pass it to the TUI so connect/copy carry -F <path>.
	if cfg := settings.ConfigPath(); cfg != "" {
		if abs, err := filepath.Abs(cfg); err == nil {
			model = model.WithConfigFlag(abs)
		}
	}
```
(`path/filepath` needs importing in main.go if absent.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ -run 'TestSSHArgs|TestSSHCommandFor' -v` → PASS
Run: `go test ./...` → the old `TestCopyHostSshCommand` asserts the previous copy format `ssh deploy@example.com -p 22`-style — update its expectation to the new explicit form (`ssh -p 22 deploy@example.com` for snapshot()'s host `web`).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tui.go internal/tui/tui_test.go cmd/sshush/main.go
git commit -m "Connect with -F for custom configs; copy an explicit ssh command"
```

---

### Task 6: Batch A docs + release v0.9.2

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `ARCHITECTURE.md`

- [ ] **Step 1: README**

In the Hosts-pane keybinding section, after the `↵` row's table, adjust the copy row description: `copy a ready-to-run ssh command (explicit -p/-i/-o flags from the host's block)`. In Features, "Connect to a host" bullet: append `With a custom config location (config_path / SSHUSH_CONFIG), connections run ssh -F <config> <alias> so aliases resolve against the right file.`

- [ ] **Step 2: CHANGELOG**

Add under `## [Unreleased]` → become `## [0.9.2] - <today>`:

```markdown
## [0.9.2] - 2026-06-XX

### Fixed
- Theme readability: a contrast gate now enforces minimum fg/bg contrast for
  every preset (nord/tokyonight/solarized-dark dim shades lightened), and all
  overlay body text is routed through theme styles instead of inheriting the
  terminal foreground.
- Keys/hosts panes render headers and rows through shared fixed columns — the
  default ★ lives in the gutter and fields no longer shift when it is absent.
- Connecting to a host honors a custom config location (`ssh -F <config>`).

### Added
- App window padding (1 row / 2 cols), dropped automatically on small terminals.
- Copying a host's ssh command now produces an explicit, shareable command
  (`-p` / `-i` / `-o` flags expanded from the host's block).
```
(update the link block at the bottom: `[0.9.2]: .../compare/v0.9.1...v0.9.2`, `[Unreleased]: .../compare/v0.9.2...HEAD`.)

- [ ] **Step 3: Verify, commit, tag**

Run: `go build ./... && go vet ./... && go test ./... && go test -tags e2e ./...` → PASS

```bash
git add README.md CHANGELOG.md
git commit -m "Document v0.9.2 readability, columns, padding, and connect fixes"
git tag -a v0.9.2 -m "v0.9.2 — theme readability, pane columns, padding, custom-config connect"
git push origin main && git push origin v0.9.2
```

---

## Batch B → v0.9.3

### Task 7: Keygen comment step

**Files:**
- Modify: `internal/tui/overlay_newkey.go`
- Test: modify `internal/tui/tui_test.go` (`TestNewKeyGenEd25519SkipsBits`), add comment-phase cases

- [ ] **Step 1: Write/adjust the failing tests**

In `tui_test.go`, update the tail of `TestNewKeyGenEd25519SkipsBits` — enter on the filename now advances to the comment phase instead of dispatching:

```go
	// filename accepted -> comment phase, prefilled with the filename.
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter})
	w = m.modal.(*newKeyWizard)
	if w.phase != nkPhaseComment {
		t.Fatalf("expected comment phase, got %d", w.phase)
	}
	if w.input.Value() != "id_ed25519" {
		t.Errorf("comment default = %q, want filename", w.input.Value())
	}
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // accept default comment
	m = out.(Model)
	if cmd == nil || m.modal != nil {
		t.Error("comment accept should dispatch keygen and close the wizard")
	}
```

Add a custom-comment test:

```go
func TestNewKeyCustomComment(t *testing.T) {
	m := New(&fakeService{model: snapshot()})
	m = feed(m, refreshedMsg{model: snapshot()})
	m = feed(m, key("n"))
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // ed25519 -> filename
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter}) // accept filename -> comment
	w := m.modal.(*newKeyWizard)
	w.input.SetValue("sj@work")
	if got := w.commentOrDefault("id_ed25519"); got != "sj@work" {
		t.Errorf("comment = %q", got)
	}
	w.input.SetValue("   ")
	if got := w.commentOrDefault("id_ed25519"); got != "id_ed25519" {
		t.Errorf("blank comment should fall back to filename, got %q", got)
	}
}
```

- [ ] **Step 2: Run — expect failure**

Run: `go test ./internal/tui/ -run 'TestNewKey' -v`
Expected: FAIL — `nkPhaseComment`, `commentOrDefault` undefined; old flow dispatches at filename.

- [ ] **Step 3: Implement in `overlay_newkey.go`**

Add the phase constant and a `name` field:

```go
const (
	nkPhaseAlgo = iota
	nkPhaseBits
	nkPhaseName
	nkPhaseComment
)
```
Struct gains `name string` (the accepted filename).

Replace `updateName`'s enter branch (move keygen dispatch into the new comment phase):

```go
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

// updateComment collects the -C comment and runs ssh-keygen interactively
// (via ExecProcess) so it can prompt for a passphrase.
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
```

Wire `Update`'s switch: `case nkPhaseName: return o.updateName(msg, m)` and `default: return o.updateComment(msg, m)` (comment is the final phase). Wire `View`: add

```go
func (o *newKeyWizard) viewComment() string {
	var b strings.Builder
	b.WriteString(tabActive.Render("Generate key ("+o.summary()+")") + "\n\n")
	b.WriteString(dimStyle.Render("  comment (-C) — enter to accept the default") + "\n")
	b.WriteString("  " + o.input.View() + "\n\n")
	b.WriteString(dimStyle.Render("  enter generate · esc cancel"))
	b.WriteString("\n")
	return b.String()
}
```
and route `case nkPhaseComment: return o.viewComment()` in `View`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ -run 'TestNewKey' -v` → PASS; then `go test ./...` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/overlay_newkey.go internal/tui/tui_test.go
git commit -m "Add a comment step to the new-key wizard"
```

---

### Task 8: `sshush install-extras` (embedded man page + completions)

**Files:**
- Move: `man/sshush.1` → `cmd/sshush/sshush.1` (`git mv`)
- Create: `cmd/sshush/extras.go`, test in `cmd/sshush/extras_test.go`
- Modify: `cmd/sshush/main.go` (subcommand + usage + update hook), `.goreleaser.yaml` (man src path), `install.sh`, `cmd/sshush/completions/sshush.{bash,zsh,fish}` (add subcommand), `cmd/sshush/sshush.1` (document it)

- [ ] **Step 1: Move the man page and fix goreleaser**

```bash
git mv man/sshush.1 cmd/sshush/sshush.1
```
In `.goreleaser.yaml` archives `files:`, change `src: man/sshush.1` → `src: cmd/sshush/sshush.1` (keep `dst: sshush.1` — brew formula and AUR paths are unchanged).
Validate: `go run github.com/goreleaser/goreleaser/v2@v2.12.3 check` → `1 configuration file(s) validated`.

- [ ] **Step 2: Write the failing test**

Create `cmd/sshush/extras_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallExtrasFreshAndRefresh(t *testing.T) {
	data := t.TempDir()
	cfg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_CONFIG_HOME", cfg)

	// Fresh install writes all four assets.
	if err := installExtras(false); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(data, "man/man1/sshush.1"),
		filepath.Join(data, "bash-completion/completions/sshush"),
		filepath.Join(data, "zsh/site-functions/_sshush"),
		filepath.Join(cfg, "fish/completions/sshush.fish"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}

	// --refresh only overwrites existing files: remove one, refresh, still gone.
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0], []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installExtras(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths[1]); !os.IsNotExist(err) {
		t.Error("--refresh must not create files that were not installed")
	}
	if b, _ := os.ReadFile(paths[0]); string(b) == "stale" {
		t.Error("--refresh should overwrite existing files with current assets")
	}
}
```

Run: `go test ./cmd/sshush/ -run TestInstallExtras -v` → FAIL (`installExtras` undefined).

- [ ] **Step 3: Implement `cmd/sshush/extras.go`**

```go
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed sshush.1
var manPage []byte

// dataHome is ${XDG_DATA_HOME:-~/.local/share}.
func dataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".local/share"
	}
	return filepath.Join(home, ".local", "share")
}

// configHome is ${XDG_CONFIG_HOME:-~/.config}.
func configHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config"
	}
	return filepath.Join(home, ".config")
}

// installExtras writes the embedded man page and shell completions to
// user-level paths. With refresh, only files that already exist are
// overwritten (the opt-in refresh `sshush update` runs); a fresh install
// writes everything. Serves manual/install.sh installs — Homebrew and AUR
// package these files themselves.
func installExtras(refresh bool) error {
	read := func(name string) []byte {
		b, err := completionFS.ReadFile("completions/" + name)
		if err != nil {
			panic("embedded completion missing: " + name) // build-time invariant
		}
		return b
	}
	assets := []struct {
		path string
		data []byte
	}{
		{filepath.Join(dataHome(), "man", "man1", "sshush.1"), manPage},
		{filepath.Join(dataHome(), "bash-completion", "completions", "sshush"), read("sshush.bash")},
		{filepath.Join(dataHome(), "zsh", "site-functions", "_sshush"), read("sshush.zsh")},
		{filepath.Join(configHome(), "fish", "completions", "sshush.fish"), read("sshush.fish")},
	}
	var wrote []string
	for _, a := range assets {
		if refresh {
			if _, err := os.Stat(a.path); err != nil {
				continue // not previously installed: refresh leaves it alone
			}
		}
		if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(a.path, a.data, 0o644); err != nil {
			return err
		}
		wrote = append(wrote, a.path)
	}
	for _, p := range wrote {
		fmt.Println("installed", p)
	}
	if len(wrote) == 0 {
		fmt.Println("nothing to refresh (run `sshush install-extras` for a full install)")
		return nil
	}
	// zsh has no standard user completion dir; the hint is unconditional.
	fmt.Printf("zsh users: ensure fpath includes it, e.g.\n  fpath+=(%s)\n",
		filepath.Join(dataHome(), "zsh", "site-functions"))
	return nil
}
```

- [ ] **Step 4: Wire the subcommand, update hook, usage, install.sh, docs**

`cmd/sshush/main.go` — add a case to the subcommand switch (alongside "completion"):

```go
		case "install-extras":
			refresh := len(os.Args) > 2 && os.Args[2] == "--refresh"
			if err := installExtras(refresh); err != nil {
				fmt.Fprintf(os.Stderr, "sshush: %v\n", err)
				os.Exit(1)
			}
			return
```

In `selfUpdate`, after the final `fmt.Printf("updated to %s\n", ...)`:

```go
	// Refresh previously-installed man page/completions from the NEW binary
	// (this process is still the old version). Best-effort.
	if out, err := exec.Command(exe, "install-extras", "--refresh").CombinedOutput(); err == nil {
		os.Stdout.Write(out)
	}
```
(import `os/exec` in main.go.)

Usage string: add `  sshush install-extras  install man page + completions to user dirs (--refresh: update existing only)`.

`install.sh`, after the `install -m 0755 ...` line:
```sh
"$INSTALL_DIR/sshush" install-extras || true
```

Completions: add `install-extras` (+ description "install man page and completions") to the command lists in `cmd/sshush/completions/sshush.bash`, `sshush.zsh`, `sshush.fish`. Man page (`cmd/sshush/sshush.1`): add an `install-extras` entry to the COMMANDS section mirroring the usage text.

- [ ] **Step 5: Run tests + full suite**

Run: `go test ./cmd/sshush/ -run TestInstallExtras -v` → PASS
Run: `go build ./... && go vet ./... && go test ./...` → PASS
Run: `sh -n install.sh` → OK
Smoke: `XDG_DATA_HOME=$(mktemp -d) XDG_CONFIG_HOME=$(mktemp -d) go run ./cmd/sshush install-extras` → prints four `installed …` lines + zsh hint.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "Add install-extras: embedded man page and completions installer"
```

---

### Task 9: Batch B docs + release v0.9.3

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `ARCHITECTURE.md`

- [ ] **Step 1: Docs**

- README: under "Shell completions", note `sshush install-extras` installs man page + all completions to user dirs (and that `sshush update` refreshes them if installed). In the keygen feature bullet, mention the comment step.
- ARCHITECTURE: in the post-1.0 backlog table, add a row: `curated ssh-keygen options in the new-key wizard (KDF rounds, format) — deferred from the v0.9.3 comment step`. In the M26 detail status, note the overlay-text bleed item is closed (Task 2 of this plan).
- CHANGELOG:

```markdown
## [0.9.3] - 2026-06-XX

### Added
- New-key wizard asks for a key comment (`-C`), defaulting to the file name.
- `sshush install-extras` installs the embedded man page and shell completions
  to user directories; `sshush update` refreshes previously-installed copies;
  `install.sh` runs it automatically.
```
(+ link refs updated as in Task 6.)

- [ ] **Step 2: Verify, commit, tag**

Run: `go build ./... && go vet ./... && go test ./... && go test -tags e2e ./...` → PASS

```bash
git add README.md CHANGELOG.md ARCHITECTURE.md
git commit -m "Document v0.9.3 keygen comment and install-extras"
git tag -a v0.9.3 -m "v0.9.3 — keygen comment step, install-extras"
git push origin main && git push origin v0.9.3
```
