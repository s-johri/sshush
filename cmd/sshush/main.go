package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/mattn/go-isatty"
	"github.com/s-johri/sshush/internal/agent"
	"github.com/s-johri/sshush/internal/appconfig"
	"github.com/s-johri/sshush/internal/config"
	"github.com/s-johri/sshush/internal/keys"
	"github.com/s-johri/sshush/internal/service"
	"github.com/s-johri/sshush/internal/shellinit"
	"github.com/s-johri/sshush/internal/sshconfig"
	"github.com/s-johri/sshush/internal/tui"
	"github.com/s-johri/sshush/internal/watch"
)

// version is the build version, overridden at release time via
// -ldflags "-X main.version=vX.Y.Z". Unreleased builds report "dev".
var version = "dev"

// repoSlug is the GitHub repository self-update checks for releases.
const repoSlug = "s-johri/sshush"

func main() {
	if err := validateArgs(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "sshush: %v\n\n%s", err, usage)
		os.Exit(2)
	}
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
			if err := restore(len(os.Args) > 2); err != nil {
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
		case "install-extras":
			refresh := len(os.Args) > 2 // validateArgs allows only --refresh
			if err := installExtras(refresh); err != nil {
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
	// Refresh previously-installed man page/completions from the NEW binary
	// (this process is still the old version). Best-effort.
	if out, err := exec.Command(exe, "install-extras", "--refresh").CombinedOutput(); err == nil {
		os.Stdout.Write(out)
	}
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

// programOpts returns the bubbletea options for the interactive run. A
// non-empty NO_COLOR forces a profile that emits no escape sequences.
//
// The check stays explicit, on top of the detection that bubbletea does,
// because no-color.org disables color for any non-empty value while the
// detection parses the value as a boolean. That detection keeps color on for
// NO_COLOR=yes.
//
// The profile is NoTTY rather than Ascii because Ascii drops only color and
// keeps bold and underline. v1 used termenv.Ascii, which dropped all three.
func programOpts(noColor string) []tea.ProgramOption {
	if noColor != "" {
		return []tea.ProgramOption{tea.WithColorProfile(colorprofile.NoTTY)}
	}
	return nil
}

func runTUI() {
	// The interactive UI needs a real terminal; subcommands above don't.
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(os.Stderr, "sshush: a terminal is required for the interactive UI (try `sshush help`)")
		os.Exit(1)
	}
	opts := programOpts(os.Getenv("NO_COLOR"))

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

	p := tea.NewProgram(model, opts...)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sshush: %v\n", err)
		os.Exit(1)
	}
}

// validateArgs rejects arguments that a subcommand does not take, so that a
// typo such as `sshush restore --help` never runs the command. args is
// os.Args[1:]. An unknown command is left to the dispatch in main.
func validateArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	cmd, rest := args[0], args[1:]
	var allowed []string // the flags cmd takes; each one at most once
	n := 0               // how many arguments cmd takes
	switch cmd {
	case "restore":
		allowed, n = []string{"--yes", "-y"}, 1
	case "install-extras":
		allowed, n = []string{"--refresh"}, 1
	case "completion":
		return checkCount(cmd, rest, 1)
	case "load-default", "shell-init", "update", "version", "--version", "-v", "help", "-h", "--help":
	default:
		return nil
	}
	if err := checkCount(cmd, rest, n); err != nil {
		return err
	}
	for _, a := range rest {
		if !slices.Contains(allowed, a) {
			return fmt.Errorf("%s: unknown argument %q", cmd, a)
		}
	}
	return nil
}

// checkCount returns an error when cmd has more than n arguments.
func checkCount(cmd string, rest []string, n int) error {
	if len(rest) > n {
		return fmt.Errorf("%s: unexpected argument %q", cmd, rest[n])
	}
	return nil
}

// restore reverts the SSH config (and any Included files) to their backup
// snapshots. With yes it does not ask first.
func restore(yes bool) error {
	settings := appconfig.New("")
	if _, err := settings.Load(); err != nil {
		return err
	}
	warnConfig(settings)
	svc := newService(settings.SshDir(), settings.ConfigPath())
	model, err := svc.Refresh() // populates the config repo
	if err != nil {
		return err
	}
	for _, w := range model.Warnings {
		fmt.Fprintf(os.Stderr, "sshush: warning: %s\n", w)
	}
	return runRestore(svc, restoreOpts{
		yes: yes,
		tty: isatty.IsTerminal(os.Stdin.Fd()),
		in:  os.Stdin,
		out: os.Stdout,
	})
}

// restoreOpts are the inputs of runRestore.
type restoreOpts struct {
	yes bool      // restore without a prompt (--yes)
	tty bool      // in is a terminal, so a prompt can get an answer
	in  io.Reader // where the y/n answer comes from
	out io.Writer
}

// runRestore lists each backup with its time, asks y/n (unless o.yes), then
// restores. With no terminal and no --yes it writes nothing and returns an
// error, so a script never overwrites the config by accident.
func runRestore(svc service.Service, o restoreOpts) error {
	if !svc.CanRestore() {
		fmt.Fprintln(o.out, "no backup to restore (sshush writes one before its first edit)")
		return nil
	}
	backups := svc.Backups()
	fmt.Fprintln(o.out, "These backups will replace the current files:")
	for _, b := range backups {
		fmt.Fprintf(o.out, "  %s\n    backup from %s: %s\n", b.File, b.ModTime.Format("2006-01-02 15:04"), b.Path)
	}
	if !o.yes {
		if !o.tty {
			return errors.New("stdin is not a terminal: use `sshush restore --yes` to restore without a prompt")
		}
		fmt.Fprint(o.out, "Restore? [y/N] ")
		answer, _ := bufio.NewReader(o.in).ReadString('\n')
		if a := strings.TrimSpace(answer); a != "y" && a != "Y" {
			fmt.Fprintln(o.out, "restore cancelled")
			return nil
		}
	}
	files, err := svc.RestoreBackup()
	if err != nil {
		return err
	}
	fmt.Fprintf(o.out, "restored %d file(s) from backup:\n", len(files))
	for _, b := range backups {
		if slices.Contains(files, b.File) {
			fmt.Fprintf(o.out, "  %s\n    content before the restore: %s\n", b.File, b.PreRestore)
		}
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
  sshush restore      revert the SSH config to its backup (asks first; --yes: do not ask)
  sshush update       update sshush to the latest release
  sshush version      print the version
  sshush completion <shell>  print a bash/zsh/fish completion script
  sshush install-extras  install man page + completions to user dirs (--refresh: update existing only)
  sshush help         show this help
`
