package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/mattn/go-isatty"
	"github.com/muesli/termenv"
	"github.com/s-johri/sshush/internal/tui"
	"github.com/s-johri/sshush/pkg/agent"
	"github.com/s-johri/sshush/pkg/appconfig"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
	"github.com/s-johri/sshush/pkg/service"
	"github.com/s-johri/sshush/pkg/shellinit"
	"github.com/s-johri/sshush/pkg/sshconfig"
	"github.com/s-johri/sshush/pkg/watch"
)

// version is the build version, overridden at release time via
// -ldflags "-X main.version=vX.Y.Z". Unreleased builds report "dev".
var version = "dev"

// repoSlug is the GitHub repository self-update checks for releases.
const repoSlug = "s-johri/sshush"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "load-default":
			if err := loadDefault(); err != nil {
				fmt.Fprintf(os.Stderr, "sshush: %v\n", err)
				os.Exit(1)
			}
			return
		case "shell-init":
			if files, any := shellinit.Installed(); any {
				fmt.Fprintf(os.Stderr, "sshush: snippet already present in %s — appending again will duplicate it\n",
					strings.Join(files, ", "))
			}
			fmt.Print(shellinit.Snippet)
			return
		case "version", "--version", "-v":
			fmt.Printf("sshush %s\n", version)
			return
		case "restore":
			if err := restore(); err != nil {
				fmt.Fprintf(os.Stderr, "sshush: %v\n", err)
				os.Exit(1)
			}
			return
		case "completion":
			if len(os.Args) < 3 {
				fmt.Fprint(os.Stderr, completionUsage)
				os.Exit(2)
			}
			if err := completion(os.Args[2]); err != nil {
				fmt.Fprintf(os.Stderr, "sshush: %v\n", err)
				os.Exit(1)
			}
			return
		case "update":
			if err := selfUpdate(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "sshush: %v\n", err)
				os.Exit(1)
			}
			return
		case "-h", "--help", "help":
			fmt.Print(usage)
			return
		default:
			fmt.Fprintf(os.Stderr, "sshush: unknown command %q\n\n%s", os.Args[1], usage)
			os.Exit(2)
		}
	}
	runTUI()
}

// selfUpdate checks GitHub for a newer release and replaces this binary in
// place. It refuses to run for "dev" builds (no version to compare against) and
// is a no-op when already current.
func selfUpdate(ctx context.Context) error {
	if version == "dev" {
		return fmt.Errorf("self-update is only available for released builds (this is %q); install a tagged release", version)
	}
	rel, found, err := selfupdate.DetectLatest(ctx, selfupdate.ParseSlug(repoSlug))
	if err != nil {
		return fmt.Errorf("checking latest release: %w", err)
	}
	if !found {
		return fmt.Errorf("no release found for %s", repoSlug)
	}
	if rel.LessOrEqual(version) {
		fmt.Printf("already up to date (%s)\n", version)
		return nil
	}
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return err
	}
	fmt.Printf("updating %s -> %s …\n", version, rel.Version())
	if err := selfupdate.UpdateTo(ctx, rel.AssetURL, rel.AssetName, exe); err != nil {
		return fmt.Errorf("applying update: %w", err)
	}
	fmt.Printf("updated to %s\n", rel.Version())
	return nil
}

// checkLatest reports the latest release tag and whether it is newer than this
// build, for the TUI's launch update-check. Best-effort: any failure (offline,
// rate-limited, private/auth-gated releases) yields ("", false) and no notice.
func checkLatest() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rel, found, err := selfupdate.DetectLatest(ctx, selfupdate.ParseSlug(repoSlug))
	if err != nil || !found || rel == nil {
		return "", false
	}
	if rel.LessOrEqual(version) {
		return "", false
	}
	return rel.Version(), true
}

// newService builds the service against the configured SSH directory and config
// path. Empty values fall back to ~/.ssh and ~/.ssh/config; the socket always
// comes from $SSH_AUTH_SOCK.
func newService(sshDir, configPath string) service.Service {
	repo := sshconfig.New(configPath)
	repo.SshDir = sshDir
	return service.New(keys.New(sshDir), repo, agent.New(""))
}

func runTUI() {
	// The interactive UI needs a real terminal; subcommands above don't.
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(os.Stderr, "sshush: a terminal is required for the interactive UI (try `sshush help`)")
		os.Exit(1)
	}
	// Honor NO_COLOR explicitly (in addition to termenv's own detection).
	if os.Getenv("NO_COLOR") != "" {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	// App settings (default identity, SSH dir/config overrides). Best-effort.
	settings := appconfig.New("")
	if _, err := settings.Load(); err != nil {
		// Malformed config.toml: warn, then run with built-in defaults rather
		// than refusing to start.
		fmt.Fprintf(os.Stderr, "sshush: reading config: %v (using defaults)\n", err)
	}
	warnConfig(settings)

	model := tui.New(newService(settings.SshDir(), settings.ConfigPath()))
	model = model.WithSettings(settings).WithSshDir(settings.SshDir())

	// A custom config means `ssh <alias>` would resolve against the wrong
	// file; pass it to the TUI so connect/copy carry -F <path>.
	if cfg := settings.ConfigPath(); cfg != "" {
		if abs, err := filepath.Abs(cfg); err == nil {
			model = model.WithConfigFlag(abs)
		}
	}

	// Async update-check on launch: skipped for dev builds (no version to
	// compare) and when disabled via `check_updates = false`.
	if version != "dev" && settings.CheckUpdates() {
		model = model.WithUpdateCheck(checkLatest)
	}

	// Hot reload is best-effort: if the watcher can't start, run without it.
	if w, err := watch.New(); err == nil {
		defer w.Close()
		model = model.WithWatcher(w)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sshush: %v\n", err)
		os.Exit(1)
	}
}

// restore reverts the SSH config (and any Included files) to the ".bak"
// snapshots sshush wrote before its first edit, then reports what changed.
func restore() error {
	settings := appconfig.New("")
	if _, err := settings.Load(); err != nil {
		return err
	}
	warnConfig(settings)
	svc := newService(settings.SshDir(), settings.ConfigPath())
	if _, err := svc.Refresh(); err != nil { // populates the config repo
		return err
	}
	if !svc.CanRestore() {
		fmt.Println("no backup to restore (sshush writes one before its first edit)")
		return nil
	}
	files, err := svc.RestoreBackup()
	if err != nil {
		return err
	}
	fmt.Printf("restored %d file(s) from backup:\n", len(files))
	for _, f := range files {
		fmt.Printf("  %s\n", f)
	}
	return nil
}

// warnConfig prints any non-fatal config warnings (e.g. unknown keys) from the
// last Load to stderr. Used on the interactive and restore paths; skipped on the
// frequently-run load-default path to avoid per-shell noise.
func warnConfig(s *appconfig.Store) {
	for _, w := range s.Warnings() {
		fmt.Fprintf(os.Stderr, "sshush: %s\n", w)
	}
}

// loadDefault loads the configured default identities into the agent and exits.
// Intended to run from a shell startup file (see `sshush shell-init`).
func loadDefault() error {
	settings := appconfig.New("")
	if _, err := settings.Load(); err != nil {
		return err
	}
	svc := newService(settings.SshDir(), settings.ConfigPath())
	return applyDefaults(settings.DefaultIdentities(), svc)
}

// applyDefaults loads each identity into the agent via svc, skipping ones that
// are missing on disk or already loaded — cheap and safe to call on every shell.
func applyDefaults(ids []config.IdentityID, svc service.Service) error {
	if len(ids) == 0 {
		return nil
	}
	model, err := svc.Refresh()
	if err != nil {
		return err
	}
	for _, id := range ids {
		ident, ok := model.Identities[id]
		if !ok || !ident.ExistsOnDisk || ident.LoadedInAgent {
			continue
		}
		if err := svc.AddKeyToAgent(id); err != nil {
			return err
		}
	}
	return nil
}

const usage = `sshush — interactive SSH key and host manager

Usage:
  sshush              launch the interactive TUI
  sshush load-default load the configured default identity into the agent
  sshush shell-init   print a shell snippet to load the default on shell start
  sshush restore      revert the SSH config to the backup from before edits
  sshush update       update sshush to the latest release
  sshush version      print the version
  sshush completion <shell>  print a bash/zsh/fish completion script
  sshush help         show this help
`
