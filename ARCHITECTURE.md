# sshush — Architecture

Interactive CLI/TUI to switch SSH keys, inspect the agent, view hosts, and edit SSH config.

## Decisions (locked)

| Topic | Choice | Reason |
|-------|--------|--------|
| Config write | Round-trip via `github.com/kevinburke/ssh_config` AST | Preserve user comments, ordering, unknown options. Mutate nodes, not regenerate. |
| Agent read | `golang.org/x/crypto/ssh/agent` over `$SSH_AUTH_SOCK` | List + fingerprint loaded keys, no external dep. |
| Agent write | Shell to `ssh-add` / `ssh-add -d` | Terminal handles encrypted-key passphrase prompt natively. |
| Config scope | `~/.ssh/config` + `Include`'d files | Covers real setups. Writes only to user files. |
| Key gen | Shell to `ssh-keygen` | Don't reimplement key generation. |

## Layout

```
cmd/sshush/main.go     entrypoint, wires service -> tui
pkg/config/            domain model (Identity, Host, SshConfigModel) — exists
pkg/sshconfig/         parse + round-trip write (kevinburke wrapper)
pkg/keys/              scan ~/.ssh for keypairs, detect algo/comment
pkg/agent/             agent client: List (Go proto), Add/Remove (exec ssh-add)
pkg/service/           orchestrator: builds unified model, mediates mutations
internal/tui/          BubbleTea views/update — thin, no IO of its own
```

TUI never touches files/agent directly. All IO behind `pkg/service` interfaces → testable headless, mockable, hot-reload-ready.

## Interfaces (contracts)

```go
// pkg/sshconfig
type ConfigRepo interface {
    Load() (*config.SshConfigModel, error)   // parse user config + Includes
    SetHostField(h config.HostID, key, val string) error  // mutate AST node
    AddHost(config.Host) error
    DeleteHost(config.HostID) error
    Save() error                              // backup .bak, then write AST
}

// pkg/keys
type KeyScanner interface {
    Scan() ([]config.Identity, error)         // walk ~/.ssh, pair pub/priv
}

// pkg/agent
type AgentClient interface {
    List() ([]AgentKey, error)                // loaded keys + fingerprints
    Add(path string) error                    // exec ssh-add <path>
    Remove(path string) error                 // exec ssh-add -d <path>
}

// pkg/service — the only thing TUI sees
type Service interface {
    Refresh() (*config.SshConfigModel, error) // scan+parse+agent, merged
    AddKeyToAgent(config.IdentityID) error
    RemoveKeyFromAgent(config.IdentityID) error
    EditHost(config.HostID, field, val string) error  // edit + Save
    AddHost(config.Host) error
    DeleteHost(config.HostID) error
}
```

## Data flow

```
Service.Refresh():
  keys.Scan()        -> []Identity (ExistsOnDisk)
  sshconfig.Load()   -> Hosts + Identity refs
  agent.List()       -> []AgentKey
  merge by fingerprint: set Identity.LoadedInAgent / AgentFingerprint
  -> *SshConfigModel  (single source of truth, immutable snapshot to TUI)
```

TUI holds a snapshot. Mutations dispatched as `tea.Cmd` (async goroutine) → call Service → return result `tea.Msg` → TUI re-renders. Service re-Refreshes after any mutation so state stays consistent.

## Safety invariants

- **Backup before first write**: copy `config` -> `config.bak` once per session before mutating.
- **Confirm before**: every file write and every key delete.
- **Never auto-edit shell rc files**: print snippet, user pastes.
- **Degrade, never crash**: missing `$SSH_AUTH_SOCK`, unreadable key, malformed config → status message, keep running.
- **Round-trip test** is mandatory: parse→write of an unmodified file must produce byte-identical output (modulo intended edits).

## Milestones

| # | Deliverable | Risk |
|---|-------------|------|
| 0 | Libs added, package scaffold, interfaces stubbed, tests compile | none |
| 1 | `pkg/keys` scan + unit test w/ fixtures | none (read) |
| 2 | `pkg/sshconfig` parse + Includes + round-trip test | none (read) |
| 3 | `pkg/agent` List + fingerprint match | none (read) |
| 4 | `pkg/service` Refresh merges all three | none (read) |
| 5 | TUI: read-only Keys + Hosts panes | none — **first useful build** |
| 6 | Agent add/remove (switch keys) + unload-all; auto-expiring status | low (reversible) |
| 7 | Edit/add/delete host directives, Save w/ backup+confirm | **write — backup gated** |
| 8 | Add (wizard: basic + custom options) / delete host, keygen, key delete | **write/destructive — confirm gated** |
| 9 | Wildcard host (`Host *`) add/edit/delete + per-host key association | **write — surfaces wildcard blocks** |
| 10 | Hot reload (fsnotify), reconcile | medium |
| 11 | App config (~/.config/sshush), default identity | low |
| 12 | `load-default` subcommand + shell startup snippet generator (print only) | low |
| 13 | lipgloss styling pass: bordered panes, grouped help, key↔host links | none |
| 14 | README + installation instructions | none (docs) |
| 15 | Self-updating binary via GitHub releases | medium |
| 🏷 | **v0.1.0** — first tagged release: full TUI feature set + self-update | — |
| 16 | Configurable SSH dir / config path (override `~/.ssh` defaults) | low |
| 17 | Key generation with selectable algorithm (ed25519/rsa/ecdsa) + bits | low |
| 🏷 | **v0.2.0** — configurable paths + multi-algorithm keygen | — |

### Road to v1.0.0

Everything below gates `v1.0.0`: usability at scale, correctness on real-world
configs, safety nets, and distribution. Releases are tagged as features land so
users get value before 1.0; the API/config surface only freezes at the RC.

| # | Deliverable | Risk |
|---|-------------|------|
| 18 | Scrollable / paginated panes (viewport) for long key & host lists | low |
| 19 | Search / filter within Keys and Hosts panes | low |
| 🏷 | **v0.3.0** — scrolling + search | — |
| 20 | Help overlay (`?`) + `NO_COLOR`, narrow-terminal, non-TTY handling | low |
| 21 | Styling / polish pass — refine spacing, alignment, color, micro-interactions | low |
| 22 | Custom themes — user-configurable color palette | low |
| 🏷 | **v0.4.0** — help overlay + polish + theming | — |
| 23 | `Match` block + broader directive support (read/display, edit-safe) | medium |
| 🏷 | **v0.5.0** — Match/advanced directives | — |
| 24 | Restore-from-backup (undo last write) command/action | low |
| 25 | Integration tests (real agent/keygen e2e) + CI matrix (linux/macOS) | low |
| 🏷 | **v0.6.0** — undo + e2e/CI hardening | — |
| 26 | Packaging: Homebrew tap, AUR, shell completions, man page | low |
| 27 | v1.0 stabilization: error-handling audit, config schema freeze, docs/screenshots, CHANGELOG | low |
| 🏷 | **v0.9.0** — release candidate (feature-complete, schema frozen) | — |
| — | soak period: bug-fix-only patch releases (v0.9.x) from real-world use | — |
| 🏷 | **v1.0.0** — stable release (tag + announce) | — |

### Milestone 9 detail

Two related gaps in the host model:

- **Wildcard blocks**: `hostFromAST` currently drops any block whose patterns are
  all wildcards (`*`, `?`, `!`), so `Host *` defaults are invisible and uneditable.
  Surface wildcard hosts in the model (flag them, e.g. `Host.IsPattern`), list them
  in the Hosts pane, and allow add/edit/delete. Edits route through the same
  `SetHostField`/`DeleteHostField` path; `findHost` must match pattern blocks too.
  Risk: editing `Host *` changes defaults for every connection — confirm copy
  should make that explicit.
- **Key association**: let a host reference specific identities. `Host.Identities`
  already exists in the model (parsed from `IdentityFile`); add UI to attach/detach
  a key to the selected host, writing `IdentityFile <path>` directives (supports
  multiple). On the Keys pane, optionally show which hosts use each key.

### Milestone 14 detail

`README.md` covering: what sshush is, a screenshot/asciinema, feature list, install
options (`go install`, prebuilt binary, build from source), the `shell-init` snippet
for shell-startup loading, keybindings, and the `~/.config/sshush/config.toml`
settings. Keep ARCHITECTURE.md as the design doc; README is the user-facing entry.

### Milestone 15 detail

Ship versioned releases and let the binary update itself:

- **Versioning**: embed version via `-ldflags -X main.version=...`; add a `sshush
  version` subcommand. Tag releases `vX.Y.Z`.
- **Release pipeline**: GitHub Actions on tag → build cross-platform binaries
  (linux/darwin, amd64/arm64) → attach to a GitHub Release (goreleaser is the
  standard tool).
- **Self-update**: `sshush update` checks the latest release via the GitHub API,
  downloads the matching asset, verifies a checksum, and replaces the running
  binary in place (e.g. `minio/selfupdate` or `creativeprojects/go-selfupdate`).
  Risk: replacing the executable + signature/checksum verification — gate clearly,
  no auto-update without consent.

### Milestone 16 detail

Let users point sshush at a non-default SSH directory / config file. The
constructors already accept overrides (`keys.New(dir)`, `sshconfig.New(path)`,
`agent.New(sock)`); today `cmd/sshush` always passes `""` (defaults), so nothing
is user-settable except the agent socket via `$SSH_AUTH_SOCK`.

- **Source of truth**: add `ssh_dir` and `config_path` to
  `~/.config/sshush/config.toml` (appconfig). Optionally also accept
  `SSHUSH_SSH_DIR` / `SSHUSH_CONFIG` env vars or CLI flags; precedence
  flag > env > config file > default.
- **Wire-through**: `cmd/sshush` reads them and passes to `keys.New` /
  `sshconfig.New`; the TUI's hot-reload `rewatch` must watch the configured dir
  instead of hardcoded `~/.ssh`.
- **Include resolution fix**: `resolveInclude` currently resolves relative
  `Include` globs and `~` against `~/.ssh`; make it honor the configured SSH
  directory so a relocated config's includes still resolve. Likewise the
  `IdentityFile`→`IdentityID` basename mapping is dir-agnostic and is fine.
  Risk: read paths only; no new destructive surface.

### Milestone 17 detail

The new-key flow currently hardcodes ed25519. The backend already supports more:
`keys.GenerateOpts` carries `Algorithm` and `Bits`, and `keygenArgs` emits
`-t <algo>` (+ `-b <bits>` for non-ed25519). Only the TUI needs to expose it.

- **Wizard step**: before the filename prompt, add an algorithm picker
  (ed25519 / rsa / ecdsa; dsa intentionally omitted as legacy). For rsa, prompt
  bits (default 3072 or 4096); ecdsa uses a fixed curve set (256/384/521); ed25519
  takes no bits.
- **Wire-through**: pass the chosen `Algorithm`/`Bits` into
  `keys.GenerateCommand` (interactive, via `tea.ExecProcess`).
  Risk: none beyond existing keygen.

### Milestone 18 detail

Lists currently render every row; long key/host sets overflow the pane box.
Add a viewport per pane (`bubbles/viewport` or manual windowing): track a scroll
offset, keep the cursor visible, page with `pgup`/`pgdn`, show a scroll
indicator. `m.height` already plumbs through.

### Milestone 19 detail

A `/`-activated filter that narrows the active pane to matching rows
(substring/fuzzy on name, host alias, hostname, comment). Filtering is a view
concern over the existing sorted slices; `esc` clears. Pairs with M18 scrolling.

### Milestone 20 detail

- **Help overlay**: `?` opens a full keybinding reference (the grouped footer is
  a summary; this is the complete list per mode).
- **Color/term handling**: honor `NO_COLOR`; degrade on narrow terminals
  (truncate columns, hide non-essential tags) and when stdout is not a TTY.

### Milestone 21 detail

A second styling pass once the surface is feature-complete (scrolling, search,
help all in): tighten spacing/padding and column alignment, refine color
relationships and contrast, polish overlay/card framing, and unify
glyph/iconography. Pure presentation — no behavior change. Lands before theming
(M22) so the default palette it tunes becomes the baseline themes override.

Plus an **opt-in motion system** — playful, arcade-style juice:

- **Off by default.** A `[motion]` setting in `config.toml` (and an in-app
  toggle) enables it; `enabled = false` keeps the UI completely static. Always
  forced off when `NO_COLOR`/non-TTY, or when the terminal can't keep up.
- **Intensity levels** (e.g. `subtle` / `normal` / `arcade`) scale amplitude,
  frequency, and which effects run, so users dial it from a faint shimmer to full
  retro chaos.
- **Effects**: a brief color flash on successful writes / key load-unload;
  optional screen-shake on destructive actions or errors; a live "breathing"
  shimmer/pulse on the hovered row and on loaded keys (`●`); animated transitions
  for status and pane switches.
- **Architecture**: drive animations off a frame ticker (`tea.Tick`, ~30–60ms)
  that's only scheduled while an effect is active (no idle CPU burn); represent
  effects as small time-bounded state (start time, duration, kind) layered over
  the existing render — never blocking input or writes. Reuse this same frame
  loop, not a second one.

Risk: presentation only, but guard performance (cap active effects, stop the
ticker when idle) and respect the off switch unconditionally.

### Milestone 22 detail

Let users theme the UI. The styling already centralizes colors in named palette
constants (`colPrimary`, `colAccent`, …) — lift them into a `Theme` struct built
from those defaults, and let `config.toml` override any entry (hex or 256-color
index) under a `[theme]` table. Ship a couple of presets (e.g. default, mono,
high-contrast) selectable by name; unknown/partial themes fall back per-field to
defaults. Honors M20's `NO_COLOR` (theme ignored when color is off).

### Milestone 23 detail

Real configs use `Match` blocks and directives sshush doesn't model. Today
`Match` blocks round-trip on write (untouched) but aren't surfaced. Surface them
read-only first (display, flag non-editable), then allow edits where the AST
round-trips. Audit common directives (`ProxyJump`, `IdentitiesOnly`,
`AddKeysToAgent`) — they flow through `Options`, but verify display.

### Milestone 24 detail

Every write backs up to `<path>.bak` before the first change of a session (M7).
Expose an undo: restore `<path>` from `<path>.bak` (confirm-gated), so a bad edit
is one keystroke to revert. Surface in the TUI and as a `sshush restore`
subcommand.

### Milestone 25 detail

Unit tests are thorough; add e2e coverage behind a build tag: spin a real
`ssh-agent`, generate a throwaway key, exercise load/unload/delete and a config
edit against a temp `~/.ssh`. CI matrix (ubuntu + macOS) runs the full suite incl.
e2e, plus the existing `gofmt`/`go vet` gates.

### Milestone 26 detail

Distribution: a Homebrew tap (goreleaser publishes the formula), an AUR
`PKGBUILD`, shell completions (bash/zsh/fish from the subcommand set), and a man
page. Mostly goreleaser config + a tap repo.

### Milestone 27 detail

Pre-1.0 hardening: audit every error path for graceful degradation (no panics on
malformed config, missing agent, unreadable keys, permission errors); freeze the
`config.toml` schema (document it, forward-compatible parsing); add
screenshots/asciinema to the README; start a `CHANGELOG.md`. The RC (`v0.9.0`)
ships after this; `v1.0.0` follows a soak period of bug-fix-only patches.

Tests ride alongside each pkg milestone, not deferred. Parse/write corruption = worst-case bug; round-trip test guards it.
