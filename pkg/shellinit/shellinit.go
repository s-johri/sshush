// Package shellinit provides the shell-startup snippet that loads the default
// SSH identities, and detects whether it is already installed in a shell rc file
// so callers can avoid duplicate stubs and nudge users who haven't set it up.
package shellinit

import (
	"os"
	"path/filepath"
	"strings"
)

// Snippet is printed by `sshush shell-init` for the user to add to their shell
// rc. It is a no-op unless sshush is on PATH and a default identity is set.
const Snippet = `# sshush: load the default SSH identities into the agent on shell start.
# Add to your ~/.bashrc or ~/.zshrc, or run: eval "$(sshush shell-init)"
if command -v sshush >/dev/null 2>&1; then
  sshush load-default 2>/dev/null
fi
`

// marker is a stable substring of the snippet used to detect installation.
const marker = "sshush load-default"

// rcCandidates returns the shell rc files to check. Overridable in tests.
var rcCandidates = func() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".profile"),
	}
}

// Installed reports the rc files that already contain the snippet, and whether
// any do.
func Installed() (files []string, any bool) {
	for _, p := range rcCandidates() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), marker) {
			files = append(files, p)
		}
	}
	return files, len(files) > 0
}
