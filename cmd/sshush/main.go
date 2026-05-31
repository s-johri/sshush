package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	selfupdate "github.com/creativeprojects/go-selfupdate"
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

// newService builds the service against the configured SSH directory and config
// path. Empty values fall back to ~/.ssh and ~/.ssh/config; the socket always
// comes from $SSH_AUTH_SOCK.
func newService(sshDir, configPath string) service.Service {
	repo := sshconfig.New(configPath)
	repo.SshDir = sshDir
	return service.New(keys.New(sshDir), repo, agent.New(""))
}

func runTUI() {
	// App settings (default identity, SSH dir/config overrides). Best-effort.
	settings := appconfig.New("")
	_, _ = settings.Load()

	model := tui.New(newService(settings.SshDir(), settings.ConfigPath()))
	model = model.WithSettings(settings).WithSshDir(settings.SshDir())

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
  sshush update       update sshush to the latest release
  sshush version      print the version
  sshush help         show this help
`
