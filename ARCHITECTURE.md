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
cmd/cli/main.go        entrypoint, wires service -> tui
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
| 13 | lipgloss styling pass | none |

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

Tests ride alongside each pkg milestone, not deferred. Parse/write corruption = worst-case bug; round-trip test guards it.
