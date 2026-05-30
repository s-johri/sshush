package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/s-johri/sshush/internal/tui"
	"github.com/s-johri/sshush/pkg/agent"
	"github.com/s-johri/sshush/pkg/keys"
	"github.com/s-johri/sshush/pkg/service"
	"github.com/s-johri/sshush/pkg/sshconfig"
	"github.com/s-johri/sshush/pkg/watch"
)

func main() {
	// Empty paths/socket => defaults: ~/.ssh, ~/.ssh/config, $SSH_AUTH_SOCK.
	svc := service.New(
		keys.New(""),
		sshconfig.New(""),
		agent.New(""),
	)

	model := tui.New(svc)
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
