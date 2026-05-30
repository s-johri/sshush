# sshush

An interactive terminal UI for managing SSH keys, the ssh-agent, and your
`~/.ssh/config` — switch keys, see what's loaded in the agent, browse hosts, and
edit your config without leaving the terminal.

> Status: in active development. Edits to your SSH config are gated behind a
> confirmation and a `.bak` backup is written before the first change, but treat
> it as pre-1.0 software.

## Features

- **Keys pane** — every key pair in `~/.ssh`, its algorithm, fingerprint, agent
  load state (`●` loaded / `○` not), the default key (`★`), and which hosts use
  it (`↪ host, …`).
- **Agent integration** — load/unload a key (`ssh-add`), unload everything
  (`ssh-add -D`), and see agent-only keys that aren't on disk.
- **Hosts pane** — hosts from `~/.ssh/config` and its `Include`d files with their
  connection details (`user@hostname:port`), including wildcard (`Host *`) blocks.
- **Edit config in place** — change `HostName`/`User`/`Port`, add/edit/delete any
  directive (e.g. `ForwardAgent yes`), add or delete whole hosts (guided wizard),
  and attach/detach keys to a host (`IdentityFile`). Formatting and comments are
  preserved; a backup is written first.
- **Generate & delete keys** — `ssh-keygen` wrapper; deleting a key also removes
  it from the agent.
- **Default identity** — mark a key as default and have it auto-loaded into the
  agent on startup (in-app, or on every shell via `sshush shell-init`).
- **Hot reload** — external changes to your config or `~/.ssh` are picked up
  automatically.

## Requirements

- Go 1.25+ (to build from source)
- `ssh-agent`, `ssh-add`, `ssh-keygen` on your `PATH`
- A running agent (`echo $SSH_AUTH_SOCK` should be non-empty) for agent features

## Install

### Build from source (recommended for now)

```bash
git clone https://github.com/s-johri/sshush.git
cd sshush
go build -o "$(go env GOPATH)/bin/sshush" ./cmd/sshush
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`.

### go install

```bash
go install github.com/s-johri/sshush/cmd/sshush@latest
```

Prebuilt release binaries are planned (see ARCHITECTURE.md, milestone 15).

## Usage

```bash
sshush              # launch the interactive TUI
sshush load-default # load the configured default identity into the agent
sshush shell-init   # print a shell snippet to load the default on shell start
sshush help         # show help
```

### Keybindings

**Keys pane**

| Key | Action |
|-----|--------|
| `↵` enter / space | load / unload the selected key in the agent |
| `U` | unload all keys from the agent |
| `s` | set / unset the selected key as the startup default |
| `n` | generate a new key (`ssh-keygen`) |
| `d` | delete the selected key's files (irreversible) |

**Hosts pane**

| Key | Action |
|-----|--------|
| `e` | edit host directives (`tab` to cycle, `ctrl+o` add option, `ctrl+d` delete) |
| `i` | attach / detach keys for the host |
| `n` | add a new host (guided wizard) |
| `d` | delete the host |

**Anywhere**

| Key | Action |
|-----|--------|
| `tab` / `←` `→` | switch panes |
| `↑` `↓` / `k` `j` | move |
| `r` | refresh |
| `q` / `ctrl+c` | quit |

Writes are confirmed with `y` / `n`; `esc` cancels an overlay.

## Load the default key on shell startup

Mark a key as default in the TUI (Keys pane → `s`), then add the snippet to your
shell rc so each new shell loads it:

```bash
sshush shell-init >> ~/.bashrc   # or ~/.zshrc
```

The snippet only runs if `sshush` is on your `PATH`, and `load-default` is a
no-op when the key is already loaded — cheap and safe to run on every shell.

## Configuration

sshush stores its own settings (separate from `~/.ssh/config`) at
`$XDG_CONFIG_HOME/sshush/config.toml` (default `~/.config/sshush/config.toml`):

```toml
default_identity = "id_ed25519"
auto_load = true
```

These are managed from the TUI; you normally don't edit this file by hand.

## How it works

sshush merges three sources into one view on each refresh:

1. key pairs scanned from `~/.ssh`,
2. hosts parsed from `~/.ssh/config` (following `Include`s),
3. identities currently loaded in the agent,

matching disk keys to agent keys by SHA256 fingerprint. Config writes go through
a round-tripping parser so comments, ordering, and unknown options survive
edits. See [ARCHITECTURE.md](ARCHITECTURE.md) for the design.

## License

MIT — see [LICENSE](LICENSE).
