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
| 16 | Configurable SSH dir / config path (override `~/.ssh` defaults) | low |
| 17 | Key generation with selectable algorithm (ed25519/rsa/ecdsa) + bits | low |

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

Tests ride alongside each pkg milestone, not deferred. Parse/write corruption = worst-case bug; round-trip test guards it.
