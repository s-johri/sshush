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
)

func main() {
	// Empty paths/socket => defaults: ~/.ssh, ~/.ssh/config, $SSH_AUTH_SOCK.
	svc := service.New(
		keys.New(""),
		sshconfig.New(""),
		agent.New(""),
	)

	p := tea.NewProgram(tui.New(svc), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sshush: %v\n", err)
		os.Exit(1)
	}
}
