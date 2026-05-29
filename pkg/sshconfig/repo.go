// Package sshconfig parses ~/.ssh/config (following Include directives) into
// the domain model and writes edits back while preserving comments, ordering,
// and unknown options via a round-tripping AST.
package sshconfig

import (
	"errors"

	"github.com/s-johri/sshush/pkg/config"
)

// ErrNotImplemented is returned by stubbed methods during scaffolding.
var ErrNotImplemented = errors.New("not implemented")

// ConfigRepo loads and mutates SSH configuration. Mutations operate on the
// in-memory AST; Save backs up the file then writes the AST.
type ConfigRepo interface {
	Load() (*config.SshConfigModel, error)
	SetHostField(h config.HostID, key, val string) error
	AddHost(config.Host) error
	DeleteHost(config.HostID) error
	Save() error
}

// FileRepo is a ConfigRepo backed by an on-disk config file plus its Includes.
type FileRepo struct {
	Path string // path to user config; empty means ~/.ssh/config
}

// New returns a FileRepo for path. Empty path defaults to ~/.ssh/config.
func New(path string) *FileRepo {
	return &FileRepo{Path: path}
}

// Load implements ConfigRepo.
func (r *FileRepo) Load() (*config.SshConfigModel, error) {
	return nil, ErrNotImplemented
}

// SetHostField implements ConfigRepo.
func (r *FileRepo) SetHostField(h config.HostID, key, val string) error {
	return ErrNotImplemented
}

// AddHost implements ConfigRepo.
func (r *FileRepo) AddHost(h config.Host) error {
	return ErrNotImplemented
}

// DeleteHost implements ConfigRepo.
func (r *FileRepo) DeleteHost(h config.HostID) error {
	return ErrNotImplemented
}

// Save implements ConfigRepo.
func (r *FileRepo) Save() error {
	return ErrNotImplemented
}
