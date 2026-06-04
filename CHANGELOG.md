# Changelog

All notable changes to sshush are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/s-johri/sshush/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/s-johri/sshush/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/s-johri/sshush/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/s-johri/sshush/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/s-johri/sshush/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/s-johri/sshush/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/s-johri/sshush/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/s-johri/sshush/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/s-johri/sshush/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/s-johri/sshush/releases/tag/v0.1.0
