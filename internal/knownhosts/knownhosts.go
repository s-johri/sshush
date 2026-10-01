// Package knownhosts reads ~/.ssh/known_hosts and removes entries. It exists to
// tame the "REMOTE HOST IDENTIFICATION HAS CHANGED" wall: list the recorded host
// keys, find the offending one, and drop it — without hand-editing the file.
package knownhosts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Entry is one recorded host key.
type Entry struct {
	Hosts       []string // host patterns; a hashed entry has one opaque token
	KeyType     string   // e.g. "ssh-ed25519"
	Fingerprint string   // SHA256 of the stored public key
	Hashed      bool     // true when the host is stored hashed (HashKnownHosts)
	Line        int      // 0-based line index in the file (for removal)
}

// Display returns a human label for the entry's host(s).
func (e Entry) Display() string {
	if e.Hashed {
		return "(hashed host)"
	}
	return strings.Join(e.Hosts, ", ")
}

// Path returns the known_hosts path under sshDir (default ~/.ssh).
func Path(sshDir string) (string, error) {
	if sshDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		sshDir = filepath.Join(home, ".ssh")
	}
	return filepath.Join(sshDir, "known_hosts"), nil
}

// Parse reads a known_hosts file into entries. A missing file yields no entries
// (not an error). Unparseable lines (blanks, comments, junk) are skipped, but
// line numbering still reflects the real file so removal targets the right line.
func Parse(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []Entry
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		_, hosts, pub, _, _, err := ssh.ParseKnownHosts([]byte(line))
		if err != nil || pub == nil {
			continue
		}
		hashed := len(hosts) > 0 && strings.HasPrefix(hosts[0], "|1|")
		entries = append(entries, Entry{
			Hosts:       hosts,
			KeyType:     pub.Type(),
			Fingerprint: ssh.FingerprintSHA256(pub),
			Hashed:      hashed,
			Line:        i,
		})
	}
	return entries, nil
}

// Remove deletes the given line from the known_hosts file, backing the file up
// to "<path>.bak" first. Works for hashed and plaintext entries alike (unlike
// ssh-keygen -R, which needs a plaintext hostname).
func Remove(path string, line int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	if line < 0 || line >= len(lines) {
		return fmt.Errorf("line %d out of range", line)
	}

	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(path+".bak", data, mode); err != nil {
		return fmt.Errorf("backup %s: %w", path, err)
	}

	kept := append(lines[:line], lines[line+1:]...)
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
