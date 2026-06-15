# Soak polish: readability, layout, connect correctness, and install extras

- Date: 2026-06-12
- Status: approved
- Scope: seven v0.9.x soak findings, shipped as two patches before v1.0.0:
  **Batch A (bugs/visual) → v0.9.2**, **Batch B (small features) → v0.9.3**.

## Batch A — bugs and visual fixes (v0.9.2)

### A1. Theme readability pass

Two mechanisms, both required:

1. **Foreground-bleed fix.** Every overlay body string currently written
   unstyled (delete-confirm bodies, perms issue rows, prompt hints, confirm
   text) is routed through the theme styles (`textStyle` for body,
   `dimStyle` for secondary). This closes the deferred M26 item "route overlay
   body text through explicit theme colors": no rendered string may inherit
   the terminal's default foreground.
2. **Contrast audit gate.** A new `contrast_test.go` in `internal/tui`
   computes the WCAG-style luminance contrast ratio of each preset theme's
   roles against its `Bg` (themes without a `Bg`, i.e. the default 256-color
   theme, are skipped):
   - `Text` vs `Bg` ≥ 4.5:1
   - `Dim` and `Subtle` vs `Bg` ≥ 3:1
   - `Primary`, `Accent`, `Err`, `Gold`, `Green`, `HostTag` vs `Bg` ≥ 3:1
   - `Accent` vs `SelBg` (selected row) ≥ 3:1

   Palette values that fail (known: nord `Dim #4c566a` on `#2e3440`; others
   as caught) are adjusted to the nearest readable shade from the same
   upstream palette. The test remains as a permanent regression gate for
   future themes.

Known-bad fixtures to verify by render: delete-confirm prompt body, help
overlay text, and the footer keybind hints (`Dim`/`Subtle` roles) on nord.

### A2. Column engine for pane rows and headers

The keys-pane header and rows are currently formatted independently
(`fmt` string vs concatenation in `listRow`), so the header misaligns and
optional fields (`★ default`, `↪ hosts`, comment) shift left when absent.

Replace with one column engine that renders **both** the header and every
row, with the user-chosen layout (option B from brainstorming):

- Gutter: selection marker `▸`, loaded glyph `●`/`○`, default star `★`
  (star slot always reserved; blank when not default).
- `name` — fixed 20 cols (`padClip`).
- `algo` — fixed 11 cols.
- `hosts` — flexible width.
- `comment` — dim, takes the remainder.

Truncation priority unchanged: comment drops first, then hosts truncates
with `…`. The hosts pane gets the same engine (host 20 / destination flex),
including its header. Match-block rows keep their existing rendering through
the same columns.

### A3. App padding

Padding applied in the `View` wrapper, inside the themed background fill
(the background covers the padding): 1 blank row top and bottom, 2 columns
left and right. All capacity math (`listCapacity`, `boxInner`, overlay
capacities) accounts for the padding. Below ~40 columns of width or ~12 rows
of height the padding is dropped before any content truncates.

### A4. Connect and copy correctness with custom configs

- **Connect** (`enter` on a host): when a custom SSH config is configured
  (`config_path`/`ssh_dir` in `config.toml` or `SSHUSH_CONFIG`), run
  `ssh -F <absolute config path> <alias>` so ssh resolves HostName, wildcard
  and Match merges, ProxyJump, and identities itself with full fidelity.
  When no custom config is set, keep plain `ssh <alias>` — `-F` also
  suppresses `/etc/ssh/ssh_config`, so it must not be passed by default.
  The config path is plumbed into the TUI via a `Model` option from
  `settings.ConfigPath()`.
- **Copy** (`c` on a host): build the explicit, shareable command from the
  host's own block: `ssh -p <port> -i <identity path>… -o Key=Value…
  <user>@<hostname>` with options sorted for determinism. Hosts without a
  `HostName` keep today's alias form. (The explicit command intentionally
  reflects only the host's own block; cross-block wildcard/Match merges are
  ssh's job at connect time and are out of scope for the copied command.)

## Batch B — small features (v0.9.3)

### B1. Keygen comment step

`newKeyWizard` gains a fourth phase: algorithm → bits → filename →
**comment**. The comment input is prefilled with the filename; plain enter
accepts it (preserving today's behavior). The value flows into
`keys.GenerateOpts.Comment`. Curated keygen options (KDF rounds, format, …)
are explicitly deferred post-1.0 and recorded in ARCHITECTURE's backlog.

### B2. `sshush install-extras`

- The man page source moves to `cmd/sshush/` so it can be embedded
  (`go:embed` cannot reach above the package directory). The goreleaser
  archive destination stays `sshush.1`, so the Homebrew formula and AUR
  package are untouched.
- New subcommand `sshush install-extras` writes the embedded man page and
  completions to user-level paths:
  - man → `${XDG_DATA_HOME:-~/.local/share}/man/man1/sshush.1`
  - bash → `${XDG_DATA_HOME:-~/.local/share}/bash-completion/completions/sshush`
  - zsh → `${XDG_DATA_HOME:-~/.local/share}/zsh/site-functions/_sshush`,
    always printing a one-line hint showing the `fpath+=` line to add (zsh
    has no standard user completion dir, so the hint is unconditional)
  - fish → `${XDG_CONFIG_HOME:-~/.config}/fish/completions/sshush.fish`
- `install-extras --refresh` overwrites **only files that already exist**
  (the opt-in refresh used by update).
- `sshush update`, after a successful binary swap, executes the **new**
  binary with `install-extras --refresh` so the refreshed assets match the
  new version.
- `install.sh` calls `install-extras` after placing the binary.

This feature serves manual and `install.sh` installs; Homebrew/AUR manage
these files through their own packaging and their users do not run
`sshush update`.

## Testing

- Contrast gate across all 16 presets × roles (A1).
- Column-engine render tests asserting header/row alignment, including the
  originally-reported case of a non-default key's fields (A2).
- Padding render tests: capacity math with padding, and the narrow-terminal
  drop (A3).
- `connectToHost` argument assertions: `-F` present only with a custom
  config; absolute path used (A4).
- `sshCommand` expansion cases: port, options, multiple identities,
  determinism, no-hostname fallback (A4).
- Wizard comment phase: prefill, accept, custom value (B1).
- `install-extras` against temp `XDG_DATA_HOME`/`XDG_CONFIG_HOME`/`HOME`:
  fresh install, `--refresh` only-overwrites-existing, zsh fpath hint (B2).
- Full suite + e2e green per slice.

## Documentation

- README: connect `-F` semantics, `install-extras`, keygen comment step.
- Man page and all three completion scripts gain `install-extras`.
- CHANGELOG entries for v0.9.2 and v0.9.3.
- ARCHITECTURE: post-1.0 backlog entry for curated keygen options; note the
  M26 overlay-text item as closed by A1.
