# sshush — Architecture

Interactive CLI/TUI to switch SSH keys, inspect the agent, view hosts, and edit SSH config.

**Status:** milestones 0–17 shipped (tagged `v0.2.0`) — full read/merge pipeline,
agent switch (load/unload/unload-all), host directive + key/host CRUD with
backup+confirm, wildcard hosts, key↔host association, hot reload, app config with
default-identity auto-load, configurable SSH dir/config path, multi-algorithm key
generation, lipgloss styling, and a versioned self-update/release pipeline. The
roadmap below (M18+) carries the work to `v1.0.0` and beyond. Tests cover every
`pkg`; see [README.md](README.md) for usage.

## Decisions (locked)

| Topic | Choice | Reason |
|-------|--------|--------|
| Config write | Round-trip via `github.com/kevinburke/ssh_config` AST | Preserve user comments, ordering, unknown options. Mutate nodes, not regenerate. |
| Agent read | `golang.org/x/crypto/ssh/agent` over `$SSH_AUTH_SOCK` | List + fingerprint loaded keys, no external dep. |
| Agent write | Shell to `ssh-add` / `ssh-add -d` | Terminal handles encrypted-key passphrase prompt natively. |
| Config scope | `~/.ssh/config` + `Include`'d files | Covers real setups. Writes only to user files. |
| Key gen | Shell to `ssh-keygen` | Don't reimplement key generation. |
| App settings | TOML at `~/.config/sshush/config.toml` (BurntSushi/toml) | sshush's own prefs (default identity), separate from `~/.ssh`. |
| Hot reload | `fsnotify` watching config dirs + `~/.ssh`, debounced | Pick up external edits without a manual refresh. |
| Releases | goreleaser + GitHub Actions on `v*` tags | Cross-platform binaries + checksums, reproducible. |
| Self-update | `creativeprojects/go-selfupdate` (GitHub releases) | `sshush update`: detect, checksum-verify, replace in place. |

## Layout

```
cmd/sshush/main.go     entrypoint + subcommands (TUI, load-default, shell-init, update, version)
pkg/config/            domain model (Identity, Host, SshConfigModel)
pkg/sshconfig/         parse + round-trip write (kevinburke wrapper)
pkg/keys/              scan ~/.ssh for keypairs; generate/delete keys
pkg/agent/             agent client: List (Go proto), Add/Remove/RemoveAll (exec ssh-add)
pkg/service/           orchestrator: builds unified model, mediates mutations
pkg/appconfig/         sshush settings (default identity, SSH dir/config overrides) at ~/.config/sshush
pkg/watch/             fsnotify wrapper, debounced change signals (hot reload)
internal/tui/          BubbleTea views/update — thin, no IO of its own
```

TUI never touches files/agent directly. All IO behind `pkg/service` interfaces → testable headless, mockable, hot-reload-ready. (For interactive subprocesses — `ssh-add` on an encrypted key, `ssh-keygen` — the TUI uses `tea.ExecProcess` with command builders from `pkg/agent`/`pkg/keys`, the one sanctioned exception to "no direct IO".)

## Interfaces (contracts)

```go
// pkg/sshconfig — round-tripping config read/write
type ConfigRepo interface {
    Load() (*config.SshConfigModel, error)   // parse user config + Includes
    SetHostField(h config.HostID, key, val string) error
    DeleteHostField(h config.HostID, key string) error
    AddHostIdentity(h config.HostID, path string) error          // IdentityFile +=
    RemoveHostIdentity(h config.HostID, id config.IdentityID) error
    AddHost(config.Host) error
    DeleteHost(config.HostID) error
    Save() error                              // backup <path>.bak, then write AST
}

// pkg/keys — disk scan + key file management
type KeyScanner interface {
    Scan() ([]config.Identity, error)         // walk ~/.ssh, pair pub/priv
    Generate(GenerateOpts) (config.Identity, error)  // ssh-keygen
    Delete(privPath string) error             // remove priv + .pub
}

// pkg/agent — talk to ssh-agent
type AgentClient interface {
    List() ([]AgentKey, error)                // loaded keys + fingerprints
    Add(path string) error                    // exec ssh-add <path> (inherits tty)
    Remove(path string) error                 // exec ssh-add -d <path>
    RemoveAll() error                         // exec ssh-add -D
}

// pkg/service — the only thing TUI sees
type Service interface {
    Refresh() (*config.SshConfigModel, error) // scan+parse+agent, merged
    AddKeyToAgent(config.IdentityID) error
    RemoveKeyFromAgent(config.IdentityID) error
    UnloadAllKeys() error
    EditHost(h config.HostID, field, val string) error      // edit + Save + Refresh
    DeleteHostField(h config.HostID, field string) error
    AttachKey(h config.HostID, id config.IdentityID) error  // write IdentityFile
    DetachKey(h config.HostID, id config.IdentityID) error
    AddHost(config.Host) error
    DeleteHost(config.HostID) error
    GenerateKey(keys.GenerateOpts) (config.Identity, error)
    DeleteKey(config.IdentityID) error        // unloads from agent first
}
```

The TUI also depends on two optional narrow interfaces it can run without:
`appSettings` (default identity; backed by `pkg/appconfig`) and `fileWatcher`
(hot reload; backed by `pkg/watch`).

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

- **Backup before first write**: each file is copied to `<path>.bak` once per session before its first mutation.
- **Confirm before**: every file write and every key delete (key-file deletion is flagged irreversible).
- **Never auto-edit shell rc files**: `sshush shell-init` prints a snippet, user pastes.
- **Degrade, never crash**: missing `$SSH_AUTH_SOCK`, unreadable key, malformed config, no settings/watcher → status message, keep running.
- **Round-trip test** is mandatory: parse→write of an unmodified file must produce byte-identical output (modulo intended edits).
- **Self-induced reloads are muted**: hot reload ignores fs events caused by sshush's own writes (a short window after each mutation) so they don't spuriously re-notify.

### Fragile spots (pinned + test-guarded)

Two places use reflection to reach unexported fields of `kevinburke/ssh_config`,
**pinned to v1.6.0** and guarded by tests that fail loudly on a library bump:

- clearing a `KV`'s `rawValue` so an edited value re-renders (`setKVValue`), preserving indentation/comments;
- reading a `Host`'s `implicit` flag (`isImplicitHost`) to skip the parser's synthetic global block.

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
| 20 | Connect to host — `Enter` on a host launches `ssh <alias>` | low |
| 🏷 | **v0.3.0** — scrolling, search, connect-to-host | — |
| 21 | Permissions audit + fix (`~/.ssh`, keys, `config`, `authorized_keys` modes) | low |
| 22 | `known_hosts` management (view/search, remove stale/changed keys) | low |
| 23 | Copy to clipboard (public key / fingerprint / ready `ssh` command) | low |
| 🏷 | **v0.4.0** — perms fix, known_hosts, clipboard | — |
| 24 | Help overlay (`?`) + `NO_COLOR`, narrow-terminal, non-TTY handling | low |
| 25 | Styling / polish pass + opt-in motion system | low |
| 26 | Custom themes — user-configurable color palette | low |
| 🏷 | **v0.5.0** — help overlay + polish/motion + theming | — |
| 27 | `Match` block + broader directive support (read/display, edit-safe) | medium |
| 28 | Restore-from-backup (undo last write) command/action | low |
| 🏷 | **v0.6.0** — Match/advanced directives + undo | — |
| 29 | Integration tests (real agent/keygen e2e) + CI matrix (linux/macOS) | low |
| 30 | Packaging: Homebrew tap, AUR, shell completions, man page | low |
| 🏷 | **v0.7.0** — e2e/CI hardening + packaging | — |
| 31 | v1.0 stabilization: error-handling audit, config schema freeze, docs/screenshots, CHANGELOG | low |
| 🏷 | **v0.9.0** — release candidate (feature-complete, schema frozen) | — |
| — | soak period: bug-fix-only patch releases (v0.9.x) from real-world use | — |
| 🏷 | **v1.0.0** — stable release (tag + announce) | — |

### Beyond v1.0 — planned features

Daily-friction SSH features that ship after 1.0; each lands as a clear `v1.x`
bump. The "why it hurts" column is the user pain sshush removes.

| # | Feature | Why it hurts today | Target |
|---|---------|--------------------|--------|
| 32 | Install key on remote (`ssh-copy-id`) | manual append to a remote `authorized_keys` | v1.1.0 |
| 33 | Agent add with lifetime / confirm (`ssh-add -t/-c`) | keys live forever in the agent; no per-use confirm | v1.1.0 |
| 34 | Change / add key passphrase (`ssh-keygen -p`) | obscure syntax; unclear which keys are encrypted | v1.2.0 |
| 35 | Key hygiene warnings (weak / old / orphan) | weak (RSA<2048, DSA), aging, and orphaned keys pile up unnoticed | v1.2.0 |
| 36 | Fingerprint + randomart view (`ssh-keygen -lv`) | verifying a server key by eye is fiddly | v1.3.0 |
| 37 | Connection / auth test (up/down/auth badge) | "will it even connect?" needs a manual attempt | v1.3.0 |
| 38 | Backup & restore (export/import keys + config) | moving to a new machine is manual and risky | v1.4.0 |

Connectivity actions (20, 32, 37) reuse the `tea.ExecProcess` terminal-handover
pattern already used for `ssh-add`/`ssh-keygen`.

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

sshush manages SSH but can't yet *do* SSH. Make `Enter` on a host launch
`ssh <alias>` via `tea.ExecProcess` — the TUI yields the terminal, the session
runs, and sshush resumes when it exits. Same terminal-handover pattern as
`ssh-add`/`ssh-keygen`. Use the host's alias so the user's own config (incl.
`ProxyJump`, etc.) applies. Optional: a key to copy the equivalent command.

### Milestone 21 detail

SSH silently refuses keys and `config` files with loose permissions, producing
cryptic auth failures. Audit modes on `~/.ssh` (700), private keys (600), `config`
and `authorized_keys`; surface offenders with a warning badge and offer a
one-key `chmod` fix (confirm-gated). Read-only detection is safe; the fix is the
only write and is reversible in spirit (tightening perms).

### Milestone 22 detail

The `REMOTE HOST IDENTIFICATION HAS CHANGED` wall blocks logins and editing
`known_hosts` by hand is error-prone. Parse `~/.ssh/known_hosts` (+ `known_hosts2`,
hashed entries), list/search entries, show the associated host, and remove a
stale/changed entry via `ssh-keygen -R <host>` (confirm-gated). Read-first, then
the targeted removal.

### Milestone 23 detail

Pasting the right public key into GitHub/servers means hunting for the `.pub`.
Add clipboard actions (via an OSC 52 escape or a clipboard lib): copy a key's
public key, its SHA256 fingerprint, or a ready-to-run `ssh <user>@<host> -p <port>
-i <path>` command for the selected host. Degrade gracefully where no clipboard
is available (print to status / stdout).

### Milestone 24 detail

- **Help overlay**: `?` opens a full keybinding reference (the grouped footer is
  a summary; this is the complete list per mode).
- **Color/term handling**: honor `NO_COLOR`; degrade on narrow terminals
  (truncate columns, hide non-essential tags) and when stdout is not a TTY.

### Milestone 25 detail

A second styling pass once the surface is feature-complete (scrolling, search,
connect, help all in): tighten spacing/padding and column alignment, refine color
relationships and contrast, polish overlay/card framing, and unify
glyph/iconography. Pure presentation — no behavior change. Lands before theming
(M26) so the default palette it tunes becomes the baseline themes override.

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

### Milestone 26 detail

Let users theme the UI. The styling already centralizes colors in named palette
constants (`colPrimary`, `colAccent`, …) — lift them into a `Theme` struct built
from those defaults, and let `config.toml` override any entry (hex or 256-color
index) under a `[theme]` table. Ship a couple of presets (e.g. default, mono,
high-contrast) selectable by name; unknown/partial themes fall back per-field to
defaults. Honors M24's `NO_COLOR` (theme ignored when color is off).

### Milestone 27 detail

Real configs use `Match` blocks and directives sshush doesn't model. Today
`Match` blocks round-trip on write (untouched) but aren't surfaced. Surface them
read-only first (display, flag non-editable), then allow edits where the AST
round-trips. Audit common directives (`ProxyJump`, `IdentitiesOnly`,
`AddKeysToAgent`) — they flow through `Options`, but verify display.

### Milestone 28 detail

Every write backs up to `<path>.bak` before the first change of a session (M7).
Expose an undo: restore `<path>` from `<path>.bak` (confirm-gated), so a bad edit
is one keystroke to revert. Surface in the TUI and as a `sshush restore`
subcommand.

### Milestone 29 detail

Unit tests are thorough; add e2e coverage behind a build tag: spin a real
`ssh-agent`, generate a throwaway key, exercise load/unload/delete and a config
edit against a temp `~/.ssh`. CI matrix (ubuntu + macOS) runs the full suite incl.
e2e, plus the existing `gofmt`/`go vet` gates.

### Milestone 30 detail

Distribution: a Homebrew tap (goreleaser publishes the formula), an AUR
`PKGBUILD`, shell completions (bash/zsh/fish from the subcommand set), and a man
page. Mostly goreleaser config + a tap repo.

### Milestone 31 detail

Pre-1.0 hardening: audit every error path for graceful degradation (no panics on
malformed config, missing agent, unreadable keys, permission errors); freeze the
`config.toml` schema (document it, forward-compatible parsing); add
screenshots/asciinema to the README; start a `CHANGELOG.md`. The RC (`v0.9.0`)
ships after this; `v1.0.0` follows a soak period of bug-fix-only patches.

### Milestones 32–38 (post-1.0)

See the "Beyond v1.0" table above. Brief notes:

- **32 ssh-copy-id**: pick key + host → `ssh-copy-id -i <pub> <user@host>` via
  `tea.ExecProcess`.
- **33 agent lifetime/confirm**: load via `ssh-add -t <ttl>` and `-c`; show
  remaining lifetime in the Keys pane (needs agent key-lifetime introspection).
- **34 passphrase**: detect encrypted vs plaintext private keys; change/add/remove
  passphrase with `ssh-keygen -p` (interactive).
- **35 hygiene**: flag weak (RSA<2048, DSA), aging (by mtime), and orphan keys
  (`.pub` without private; keys no host references — reuse `hostsByKey`).
- **36 fingerprint/randomart**: `ssh-keygen -lv -f <key>` in a detail overlay.
- **37 conn-test**: `ssh -o BatchMode=yes -o ConnectTimeout=… -T <host>` →
  up/down/auth badge; run async, never block the UI.
- **38 backup/restore**: tar (optionally age/gpg-encrypted) of keys + config;
  restore with collision/perms checks.

Tests ride alongside each pkg milestone, not deferred. Parse/write corruption = worst-case bug; round-trip test guards it.
