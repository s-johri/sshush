// Package keys scans the filesystem for SSH key pairs and reports them as
// config.Identity values. It performs no mutation; key generation/deletion
// lives elsewhere.
package keys

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/s-johri/sshush/pkg/config"
	"golang.org/x/crypto/ssh"
)

// ErrNotImplemented is returned by stubbed methods during scaffolding.
var ErrNotImplemented = errors.New("not implemented")

// KeyScanner walks an SSH directory and pairs public/private keys into
// identities, detecting algorithm and comment.
type KeyScanner interface {
	Scan() ([]config.Identity, error)
}

// DiskScanner scans a directory on disk (default ~/.ssh).
type DiskScanner struct {
	Dir string // directory to scan; empty means ~/.ssh
}

// New returns a DiskScanner for dir. Empty dir defaults to ~/.ssh at Scan time.
func New(dir string) *DiskScanner {
	return &DiskScanner{Dir: dir}
}

// nonKeyNames are files in ~/.ssh that are never identities.
var nonKeyNames = map[string]bool{
	"config":          true,
	"known_hosts":     true,
	"authorized_keys": true,
}

// Scan discovers key pairs by locating *.pub files, parsing each public key,
// and pairing it with a private key of the same base name. A missing private
// key is reported via ExistsOnDisk=false rather than skipped. Unparseable or
// non-key files are ignored. The directory not existing yields an empty slice,
// not an error.
func (s *DiskScanner) Scan() ([]config.Identity, error) {
	dir, err := s.resolveDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var ids []config.Identity
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".pub") || nonKeyNames[name] {
			continue
		}

		pubPath := filepath.Join(dir, name)
		data, err := os.ReadFile(pubPath)
		if err != nil {
			continue // unreadable pub: skip, don't fail the whole scan
		}

		pub, comment, _, _, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			continue // not a valid public key
		}

		base := strings.TrimSuffix(name, ".pub")
		privPath := filepath.Join(dir, base)
		_, statErr := os.Stat(privPath)

		ids = append(ids, config.Identity{
			ID:            config.IdentityID(base),
			Name:          base,
			Path:          privPath,
			PublicKeyPath: pubPath,
			Algorithm:     algorithmFor(pub.Type()),
			Comment:       comment,
			Fingerprint:   ssh.FingerprintSHA256(pub),
			ExistsOnDisk:  statErr == nil,
		})
	}

	return ids, nil
}

// resolveDir returns s.Dir or ~/.ssh when empty.
func (s *DiskScanner) resolveDir() (string, error) {
	if s.Dir != "" {
		return s.Dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh"), nil
}

// algorithmFor maps an ssh public-key type to a KeyAlgorithm.
func algorithmFor(keyType string) config.KeyAlgorithm {
	switch {
	case keyType == ssh.KeyAlgoED25519:
		return config.AlgED25519
	case keyType == ssh.KeyAlgoRSA:
		return config.AlgRSA
	case keyType == ssh.KeyAlgoDSA:
		return config.AlgDSA
	case strings.HasPrefix(keyType, "ecdsa-sha2-"):
		return config.AlgECDSA
	default:
		return config.AlgUnknown
	}
}
