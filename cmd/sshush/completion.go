package main

import (
	"embed"
	"fmt"
	"os"
)

// completionFS holds the shell completion scripts, also shipped as files in
// release archives (and installed by the Homebrew/AUR packaging). Embedding them
// keeps `sshush completion <shell>` and the packaged files in sync.
//
//go:embed completions/sshush.bash completions/sshush.zsh completions/sshush.fish
var completionFS embed.FS

// completion prints the completion script for the named shell.
func completion(shell string) error {
	var file string
	switch shell {
	case "bash":
		file = "completions/sshush.bash"
	case "zsh":
		file = "completions/sshush.zsh"
	case "fish":
		file = "completions/sshush.fish"
	default:
		return fmt.Errorf("unknown shell %q (want bash, zsh, or fish)", shell)
	}
	data, err := completionFS.ReadFile(file)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

const completionUsage = `sshush completion <shell>

Print a shell completion script. Supported shells: bash, zsh, fish.

  bash:  sshush completion bash > /etc/bash_completion.d/sshush
  zsh:   sshush completion zsh  > "${fpath[1]}/_sshush"
  fish:  sshush completion fish > ~/.config/fish/completions/sshush.fish
`
