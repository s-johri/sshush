# Changelog

All notable changes to sshush are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- Backups no longer go next to the config file. Before, an edit of a file in
  `config.d/` wrote `config.d/<file>.bak`, and an `Include config.d/*` glob
  matched it. Then ssh applied the old values from the backup (for example a
  removed `ProxyJump` stayed in effect), and sshush showed the old values after
  a save. Backups now go to `$XDG_STATE_HOME/sshush/backups/` (default
  `~/.local/state/sshush/backups/`).
- sshush does not load a `.bak` file that an `Include` glob matches. It shows a
  warning, because ssh still reads that file: remove it by hand.
- `restore` still finds a `<file>.bak` next to the config file from an earlier
  version.
- `sshush restore` now shows each backup with its time and asks y/n before it
  writes. Use `--yes` to restore without a prompt. Without `--yes`, it refuses
  when stdin is not a terminal. Before, it wrote with no confirmation.
- A restore first saves the current content of each file as
  `<backup>.pre-restore`, so a restore can be undone.
- Every subcommand rejects arguments that it does not take, with exit code 2.
  Before, `sshush restore --help` ran a restore, and `install-extras --refres`
  ran a full install.
- An `Include` with a quoted path that has spaces (`Include "My Configs/work"`)
  now loads that file. Before, sshush split the path at the space. A relative
  `Include` resolves against the configured SSH directory (`ssh_dir`, default
  `~/.ssh`).
- Saves and restores of the SSH config, backups, `config.toml` and
  `known_hosts` now write a temp file and rename it into place, so a crash or a
  full disk never leaves a half-written file. A symlinked file (for example
  `~/.ssh/config` linked into a dotfiles repo) keeps its link, and its target
  gets the new content. The file mode is kept.
- Fixing permissions (`P`) now tries every file, also after one fails, and
  says how many were fixed and which failed. Before, it stopped at the first
  error and said only "fix failed", although it had changed some files.
- The copy preview no longer cuts a multi-byte character in two.
- A host with more than one `LocalForward`, `RemoteForward`,
  `DynamicForward`, `SendEnv`, `SetEnv` or `CertificateFile` line now keeps
  every value. The copied `ssh` command has one `-o` for each. Before, only the
  last one was kept.
- For a directive that ssh reads once (`HostName`, `User`, `Port`, `ProxyJump`
  and the others), sshush now shows the first value, as ssh uses it. Before, it
  showed the last one.
- Adding a repeatable directive with `ctrl+o` adds a line, also when the host
  already has one. The edit screen does not edit or delete a directive that
  has more than one line; it says to edit those lines in the config file.
- The copied `ssh` command for a host now uses each `IdentityFile` path as
  written in the config. Before, it looked up a key by file name, so a key in
  another directory with the same name could be used instead.
- The copied command expands `%h` and `%%` in `HostName`, as ssh does, and
  shell-quotes `user@host`, the `-F` path and the alias.
- Connect runs `ssh -- <alias>`, so an alias that starts with `-` is not read
  as an ssh option.
- `known_hosts.bak` is now written once per session, before the first removal.
  Before, each removal overwrote it, so after a second removal the first one
  could not be undone.
- Removing a `known_hosts` entry now checks that its line did not change since
  the list was read. Before, if ssh added a host in the meantime, sshush could
  remove the wrong line.
- sshush no longer writes a config line that makes every ssh connection fail.
  A new option name must be an ssh_config(5) keyword (`ForwadAgent` is
  refused), `Port` must be 1 to 65535, `HostName`, `User` and a new alias must
  be one word, and no value can span lines. An existing directive that sshush
  does not know (for example one under `IgnoreUnknown`) can still be edited.
- Before a save, sshush runs `ssh -G` on the new file and refuses a change that
  ssh rejects. It does not refuse when the file already failed before the
  change, and it does not run for a file with `Match exec`.
- The new-host wizard now shows the block and asks y/n before it writes.
- Before the first save of a config file, the confirm step says when the save
  also changes the formatting of other lines (tabs, runs of spaces,
  `Key=value`, keyword case, CRLF). ssh reads the same values. The attach and
  detach screen shows the same note.
- sshush refuses a save that would change a value that you did not edit. For
  example, `User u#x` would be written back as `User u #x`, which ssh reads as
  `u`. The README no longer says that formatting is always kept.
- A config with a `Match` block that uses `user`, `exec`, `originalhost`,
  `localuser`, `canonical`, `final`, `localnetwork`, `tagged`, `version` or
  `!host` now loads. Before, sshush could not load it, `load-default` and
  `restore` exited 1, and the Keys pane was empty. Such a block shows as
  read-only, and a save writes it back byte for byte.
- When the SSH config does not load (for example an `Include`d file that
  cannot be read), keys and the agent still work, and `load-default` still
  loads the default keys. The TUI shows why the config did not load, and
  config changes are refused until it loads. `restore` still works.
- For a host alias in more than one block, sshush now shows, edits and deletes
  the first block in the order OpenSSH reads the files (an `Include` counts at
  its line). This is the block that ssh uses. Before, sshush showed the last
  block but edited the first one, so an edit could seem to do nothing. The
  Hosts pane marks such an alias with "+N duplicate".
- With `ssh_dir` set, the new-key wizard now creates the key in that
  directory. Before, it always used `~/.ssh`.
- With `config_path` set to a file outside the SSH directory (for example in a
  dotfiles repo), the permission audit and known_hosts now use the SSH
  directory (`ssh_dir`, default `~/.ssh`). Before, they used the directory of
  the config file, so the audit could offer `chmod 700` on that directory.
- A new key name must be a plain file name: sshush rejects a name with `/`,
  `\` or `..`.
- When `config.toml` does not load (for example a TOML syntax error), sshush
  no longer writes its defaults over the file when you press `t`, `s` or `m`.
  It shows the reason in the TUI and refuses to save until you fix the file.
- Saving `config.toml` keeps keys that this version does not know, writes
  through a temp file and a rename, keeps the file mode, and writes through a
  symlink instead of replacing it. Comments in the file are still lost on save.
- A reload (`r` or a file change) and an edit no longer run at the same time.
  Before, an edit could be lost while sshush reported it as saved, or sshush
  could crash with "concurrent map writes". `r` does nothing while a load runs.
- Paste works again in the filter, the edit screen, the new-host wizard and the
  name and comment steps of the new-key wizard. It stopped working in 0.10.0.
- The `R` screen shows the time of each backup. It no longer says that the
  backup is from the start of this session, because it can be older.

### Security
- `sshush update` now checks the downloaded archive against the `checksums.txt`
  of the release before it replaces the binary. It refuses a release with a
  wrong checksum or with no `checksums.txt`. Before, it did not check the
  download. The update notice in the TUI uses the same check.
- Release binaries are built with the latest stable Go, not the minimum version
  in `go.mod`, so they get every standard library security fix. The release job
  runs vet and the tests before it publishes.
- CI runs `govulncheck`. A vulnerability that the code calls fails CI, unless
  it has no fix yet and is listed with a reason in `ci.yml`.

### Removed
- `sshush -v` no longer prints the version, and exits 2 as an unknown command.
  Use `sshush version` or `sshush --version`. This keeps `-v` free for a later
  `--verbose` within 1.x.

### Changed
- The `usage` text says "default identities" (plural), as sshush loads all of
  them.
- The Go packages moved from `pkg/` to `internal/`. sshush has no public Go
  API. Only the CLI and the `config.toml` schema are stable.

## [0.10.0] - 2026-08-18

### Changed
- The TUI now uses the Charm v2 stack (bubbletea, lipgloss and bubbles v2). The
  look and the key bindings do not change.
- `NO_COLOR` still disables color for any non-empty value (for example
  `NO_COLOR=yes`), and it also removes bold and underline, as in earlier
  releases.

## [0.9.3] - 2026-07-12

### Fixed
- The config `.bak` now survives the reload that follows each save. Before, the
  second edit in a session could overwrite the `.bak` with already-edited
  content, so restore did not go back to the original file.
- When the config changes outside sshush (an editor, or a restore), the next
  save makes a new `.bak` of that state. If the `.bak` is deleted during a
  session, the next save writes it again.
- Box borders are easier to see in the gruvbox-light, solarized-dark,
  solarized-light and tokyonight-day themes. The contrast gate now checks the
  border color too.

## [0.9.2] - 2026-06-14

### Added
- New-key wizard now asks for a key comment (`-C`), defaulting to the file name.
- `sshush install-extras` installs the embedded man page and the bash/zsh/fish
  completion scripts to user-level directories (XDG paths). `sshush update`
  refreshes previously-installed copies, and `install.sh` runs it automatically.

### Changed
- Connecting to a host under a custom config location (`config_path` /
  `SSHUSH_CONFIG` / `ssh_dir`) now runs `ssh -F <config> <alias>` so aliases
  resolve against the right file with full wildcard/`Match` fidelity. The stock
  `~/.ssh/config` still connects with plain `ssh <alias>` (so `-F` does not
  suppress `/etc/ssh/ssh_config`).
- Copying a host command (`c`) now produces an explicit, shareable invocation
  expanded from the host's own block (`-p` / `-i` / `-o` flags), shell-quoted so
  it pastes safely.

### Fixed
- Theme readability: a contrast gate now enforces minimum WCAG contrast for every
  built-in preset, and low-contrast dim/subtle/accent shades (nord, tokyonight,
  solarized, catppuccin, …) were corrected so footer and help text stay legible.
- Overlay body text now routes through the theme's colors instead of inheriting
  the terminal's default foreground, which was unreadable in some themes.
- Keys and hosts pane headers and rows now render through one shared set of fixed
  columns, so headers stay aligned and a key's fields no longer shift when it is
  not the default (the default `★` moved into the gutter).
- The app now has padding around its content instead of sitting flush against the
  terminal edges; the padding is dropped automatically on very small terminals.

## [0.9.1] - 2026-06-11

### Changed
- Internal restructuring, with no change in behavior: every modal screen (copy
  menu, theme picker, known_hosts browser, permissions, help, restore, key
  picker, delete confirms, new-key and new-host wizards, host editor) now uses
  one overlay interface, and pane scrolling moved into a shared viewport
  module. All access to the ssh_config library's private fields is in one
  adapter file, with a test that fails on an incompatible library upgrade.
- The README lists the current features (connect, copy, permissions, themes,
  `Match` blocks).

## [0.9.0] - 2026-06-04

### Added
- A demo GIF in the README, generated from a vhs tape.

### Changed
- The `config.toml` schema is frozen: within 1.x, keys are only added, never
  removed or changed in meaning.

### Fixed
- Unknown keys in `config.toml` now print a warning on launch and on
  `restore`, so a typo is visible. They are still ignored.
- A `config.toml` that does not load now prints a warning on launch and sshush
  runs with defaults. Before, the error was not shown.

## [0.7.0] - 2026-06-04

### Added
- Shell completions for bash, zsh, and fish, plus a `sshush completion <shell>`
  subcommand to print them.
- A `man` page (`man/sshush.1`), packaged in release archives.
- A `curl | bash` install script (`install.sh`) that detects OS/arch, verifies
  the release checksum, and installs the binary.
- Async update-check on launch: a transient "update available" status, off for
  `dev` builds and toggleable with `check_updates` in `config.toml`.
- End-to-end test suite (behind the `e2e` build tag) exercising a real
  `ssh-agent`, key, and config; CI now runs an ubuntu + macOS matrix.
- goreleaser configuration for a Homebrew tap and an AUR package.

## [0.6.0] - 2026-06-04

### Added
- `Match` blocks are surfaced read-only in the Hosts pane (criteria shown, edits
  refused) instead of being mis-rendered as ordinary hosts.
- Restore-from-backup: `R` in the TUI (confirm-gated) and a `sshush restore`
  subcommand revert the config to the pre-edit `.bak` snapshot.

### Fixed
- Hosts-pane alignment for `Match` blocks and long host names (fixed-width name
  column).

## [0.5.1] - 2026-06-02

### Fixed
- Theme contrast on light presets: on-fill text (tabs, flashes) now picks black
  or white by luminance instead of hardcoded black.
- `NO_COLOR` rendering no longer emits stray reset escapes when a background
  theme is set.

## [0.5.0] - 2026-06-02

### Added
- Full keybinding help overlay (`?`).
- Opt-in motion/animation system with intensity levels (`m`).
- 16 color themes (foreground + background) with an in-app live-preview switcher
  (`t`), randomize, and reset.

### Changed
- Reverted the experimental two-column adaptive layout to a full-height single
  pane (it truncated host tags and felt cramped).

## [0.4.0] - 2026-05-31

### Added
- Permission audit and fix for `~/.ssh` and key files (`P`).
- `known_hosts` browser with stale-entry removal (`K`).
- Clipboard copy of public key / fingerprint / ready `ssh` command (`c`).
- Multiple default identities, auto-loaded on startup.
- Smart `shell-init` that detects an already-installed snippet.

## [0.3.1] - 2026-05-31

### Fixed
- `ctrl+c` now quits from overlays and the filter input.
- Cursor clamping when a filter shrinks the active list.
- Deterministic ordering when writing host directives.

## [0.3.0] - 2026-05-31

### Added
- Scrollable panes (`PgUp`/`PgDn`, `g`/`G`).
- Live search/filter of the active pane (`/`).
- Connect to a host with `Enter` (`ssh <alias>` via terminal handover).

## [0.2.0] - 2026-05-31

### Added
- Configurable SSH directory and config path via environment and `config.toml`.
- Multi-algorithm key generation (ed25519 / rsa / ecdsa) with a guided wizard.

## [0.1.0] - 2026-05-30

### Added
- Initial release: an interactive TUI for SSH keys, the ssh-agent, and
  `~/.ssh/config` — keys and hosts panes, agent load/unload/unload-all, host and
  key CRUD with backup + confirmation, wildcard hosts, key↔host association, and
  hot reload.
- Versioned self-update (`sshush update`) and a goreleaser release pipeline.

[Unreleased]: https://github.com/s-johri/sshush/compare/v0.10.0...HEAD
[0.10.0]: https://github.com/s-johri/sshush/compare/v0.9.3...v0.10.0
[0.9.3]: https://github.com/s-johri/sshush/compare/v0.9.2...v0.9.3
[0.9.2]: https://github.com/s-johri/sshush/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/s-johri/sshush/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/s-johri/sshush/compare/v0.7.0...v0.9.0
[0.7.0]: https://github.com/s-johri/sshush/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/s-johri/sshush/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/s-johri/sshush/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/s-johri/sshush/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/s-johri/sshush/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/s-johri/sshush/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/s-johri/sshush/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/s-johri/sshush/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/s-johri/sshush/releases/tag/v0.1.0
