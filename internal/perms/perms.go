// Package perms audits and fixes filesystem permissions that SSH cares about.
// OpenSSH silently refuses keys and config files that are too permissive (e.g.
// a private key readable by group/other, or a config writable by others),
// producing cryptic auth failures. This package detects those and offers a
// one-call chmod fix.
package perms

import (
	"fmt"
	"os"
	"path/filepath"
)

// Kind classifies what a path is, which determines the rule applied.
type Kind int

const (
	DirKind    Kind = iota // the ~/.ssh directory: no group/other access (0700)
	KeyKind                // a private key: no group/other access (0600)
	ConfigKind             // config / authorized_keys: not writable by others (0600)
)

func (k Kind) String() string {
	switch k {
	case DirKind:
		return "directory"
	case KeyKind:
		return "private key"
	default:
		return "config"
	}
}

// Issue is a single permission problem and its suggested fix.
type Issue struct {
	Path string
	Kind Kind
	Got  os.FileMode // current permission bits
	Want os.FileMode // suggested permission bits
	Why  string
}

// Audit checks the SSH directory, config files, private keys, and an
// authorized_keys file (if present) for over-permissive modes. Missing paths
// are skipped. Paths are deduplicated; results follow dir → configs → keys
// order.
func Audit(sshDir string, configFiles, keyPaths []string) []Issue {
	var issues []Issue
	seen := map[string]bool{}

	check := func(path string, kind Kind) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		fi, err := os.Stat(path)
		if err != nil {
			return // missing/unreadable: not our problem to flag
		}
		got := fi.Mode().Perm()
		if want, why, bad := evaluate(kind, got); bad {
			issues = append(issues, Issue{Path: path, Kind: kind, Got: got, Want: want, Why: why})
		}
	}

	if sshDir != "" {
		check(sshDir, DirKind)
		check(filepath.Join(sshDir, "authorized_keys"), ConfigKind)
	}
	for _, c := range configFiles {
		check(c, ConfigKind)
	}
	for _, k := range keyPaths {
		check(k, KeyKind)
	}
	return issues
}

// evaluate returns the desired mode, a reason, and whether the current mode is
// too permissive for the kind.
func evaluate(kind Kind, got os.FileMode) (want os.FileMode, why string, bad bool) {
	switch kind {
	case DirKind:
		if got&0o077 != 0 {
			return 0o700, "accessible by group/other (ssh wants 0700)", true
		}
	case KeyKind:
		if got&0o077 != 0 {
			return 0o600, "readable/writable by group/other (ssh wants 0600)", true
		}
	case ConfigKind:
		if got&0o022 != 0 {
			return 0o600, "writable by group/other (ssh wants 0600)", true
		}
	}
	return got, "", false
}

// Fix applies the suggested permissions for an issue.
func Fix(i Issue) error {
	if err := os.Chmod(i.Path, i.Want); err != nil {
		return fmt.Errorf("chmod %s: %w", i.Path, err)
	}
	return nil
}
