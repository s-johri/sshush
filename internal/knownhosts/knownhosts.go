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
	Raw         string   // the line's text, so removal can check it did not change
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
			Raw:         line,
		})
	}
	return entries, nil
}

// Remover deletes known_hosts entries. It backs up each file to
// "<path>.bak" once per session (its lifetime), so the .bak is the file from
// before the first removal, not from before the last one. The zero value is
// ready to use.
type Remover struct {
	backedUp map[string]bool
}

// Remove deletes entry e from the known_hosts file at path. Works for hashed
// and plaintext entries alike (unlike ssh-keygen -R, which needs a plaintext
// hostname). It refuses when the line is no longer e (the file changed
// after it was read), so it never deletes another entry.
func (r *Remover) Remove(path string, e Entry) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	if e.Line < 0 || e.Line >= len(lines) || lines[e.Line] != e.Raw {
		return fmt.Errorf("%s changed since it was read; reload the list and try again", path)
	}

	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := r.backup(path, data, mode); err != nil {
		return err
	}

	kept := append(lines[:e.Line], lines[e.Line+1:]...)
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// backup writes data to "<path>.bak" on the first removal of the session,
// and again if that .bak was deleted.
func (r *Remover) backup(path string, data []byte, mode os.FileMode) error {
	bak := path + ".bak"
	if r.backedUp[path] {
		if _, err := os.Stat(bak); err == nil {
			return nil
		}
	}
	if err := os.WriteFile(bak, data, mode); err != nil {
		return fmt.Errorf("backup %s: %w", path, err)
	}
	if r.backedUp == nil {
		r.backedUp = map[string]bool{}
	}
	r.backedUp[path] = true
	return nil
}
