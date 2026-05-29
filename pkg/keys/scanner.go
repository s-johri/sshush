// Package keys scans the filesystem for SSH key pairs and reports them as
// config.Identity values. It performs no mutation; key generation/deletion
// lives elsewhere.
package keys

import (
	"errors"

	"github.com/s-johri/sshush/pkg/config"
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

// Scan implements KeyScanner.
func (s *DiskScanner) Scan() ([]config.Identity, error) {
	return nil, ErrNotImplemented
}
