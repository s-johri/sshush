package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/internal/tui"
	"github.com/s-johri/sshush/pkg/agent"
	"github.com/s-johri/sshush/pkg/appconfig"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
	"github.com/s-johri/sshush/pkg/service"
	"github.com/s-johri/sshush/pkg/sshconfig"
	"github.com/s-johri/sshush/pkg/watch"
)

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
			fmt.Print(shellSnippet)
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

func newService() service.Service {
	// Empty paths/socket => defaults: ~/.ssh, ~/.ssh/config, $SSH_AUTH_SOCK.
	return service.New(keys.New(""), sshconfig.New(""), agent.New(""))
}

func runTUI() {
	model := tui.New(newService())

	// App settings (default identity). Best-effort: run without on error.
	settings := appconfig.New("")
	if _, err := settings.Load(); err == nil {
		model = model.WithSettings(settings)
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

// loadDefault loads the configured default identity into the agent and exits.
// Intended to run from a shell startup file (see `sshush shell-init`).
func loadDefault() error {
	settings := appconfig.New("")
	if _, err := settings.Load(); err != nil {
		return err
	}
	return applyDefault(settings.AutoLoad(), settings.DefaultIdentity(), newService())
}

// applyDefault loads identity id into the agent via svc. It is a no-op when
// auto-load is off, no default is set, the key is missing on disk, or it is
// already loaded — so it is cheap and safe to call on every new shell.
func applyDefault(autoLoad bool, id config.IdentityID, svc service.Service) error {
	if !autoLoad || id == "" {
		return nil
	}
	model, err := svc.Refresh()
	if err != nil {
		return err
	}
	ident, ok := model.Identities[id]
	if !ok || !ident.ExistsOnDisk || ident.LoadedInAgent {
		return nil
	}
	return svc.AddKeyToAgent(id)
}

const shellSnippet = `# sshush: load the default SSH identity into the agent on shell start.
# Add to your ~/.bashrc or ~/.zshrc, or run: eval "$(sshush shell-init)"
if command -v sshush >/dev/null 2>&1; then
  sshush load-default 2>/dev/null
fi
`

const usage = `sshush — interactive SSH key and host manager

Usage:
  sshush              launch the interactive TUI
  sshush load-default load the configured default identity into the agent
  sshush shell-init   print a shell snippet to load the default on shell start
  sshush help         show this help
`
