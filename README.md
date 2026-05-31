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
  Press `Enter` to `ssh` into the selected host.
- **Scroll & search** — panes scroll for long lists (`PgUp`/`PgDn`, `g`/`G`); `/`
  filters the active pane by name, host, comment, or algorithm.
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

### Prebuilt binary

Download the archive for your OS/arch from the
[latest release](https://github.com/s-johri/sshush/releases/latest), extract it,
and put `sshush` on your `PATH`. Once installed from a release, update in place:

```bash
sshush update    # fetches the latest release and replaces the binary
sshush version   # show the installed version
```

### Build from source

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
sshush update       # update to the latest release
sshush version      # print the installed version
sshush help         # show help
```

### Keybindings

**Keys pane**

| Key | Action |
|-----|--------|
| `↵` enter / space | load / unload the selected key in the agent |
| `U` | unload all keys from the agent |
| `s` | toggle the selected key in/out of the startup defaults |
| `n` | generate a new key (`ssh-keygen`) |
| `d` | delete the selected key's files (irreversible) |

**Hosts pane**

| Key | Action |
|-----|--------|
| `↵` enter | `ssh` into the selected host |
| `e` | edit host directives (`tab` to cycle, `ctrl+o` add option, `ctrl+d` delete) |
| `i` | attach / detach keys for the host |
| `n` | add a new host (guided wizard) |
| `d` | delete the host |

**Anywhere**

| Key | Action |
|-----|--------|
| `tab` / `←` `→` | switch panes |
| `↑` `↓` / `k` `j` | move |
| `PgUp` `PgDn`, `g` `G` | page / jump to top / bottom |
| `/` | filter the active pane (`esc` clears) |
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
no-op when the keys are already loaded — cheap and safe to run on every shell.
`shell-init` warns (on stderr) if the snippet is already in a shell rc, so you
don't add it twice; the TUI also nudges you to install it when you set a default.

## Configuration

sshush stores its own settings (separate from `~/.ssh/config`) at
`$XDG_CONFIG_HOME/sshush/config.toml` (default `~/.config/sshush/config.toml`):

```toml
# Keys auto-loaded into the agent on startup (toggle with `s` in the TUI).
default_identities = ["id_ed25519", "id_work"]

# Optional: point sshush at a non-default SSH location.
# ssh_dir resolves relative Includes and ~ in the config; config_path defaults
# to <ssh_dir>/config when only ssh_dir is set.
ssh_dir = "~/.ssh"
config_path = "~/.ssh/config"
```

`default_identities` is managed from the TUI (`s` toggles a key in/out; all are
loaded on startup). An older `default_identity = "..."` is migrated automatically.
The path overrides can also come from the environment (which takes precedence):

```bash
export SSHUSH_SSH_DIR=~/work/.ssh
export SSHUSH_CONFIG=~/work/.ssh/config
```

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
