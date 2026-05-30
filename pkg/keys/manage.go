package keys

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/s-johri/sshush/pkg/config"
)

// GenerateOpts describes a key to create with ssh-keygen.
type GenerateOpts struct {
	Dir       string // target directory; empty means the scanner's dir (~/.ssh)
	Name      string // private key file name, e.g. "id_ed25519"
	Comment   string // -C comment
	Algorithm config.KeyAlgorithm
	Bits      int // -b; ignored for ed25519
}

// keygenArgs builds the ssh-keygen argument list for opts at privPath. Passing
// noPassphrase adds -N "" so the call does not prompt (used for headless gen).
func keygenArgs(opts GenerateOpts, privPath string, noPassphrase bool) []string {
	args := []string{"-t", string(opts.Algorithm), "-f", privPath, "-C", opts.Comment}
	if opts.Bits > 0 && opts.Algorithm != config.AlgED25519 {
		args = append(args, "-b", strconv.Itoa(opts.Bits))
	}
	if noPassphrase {
		args = append(args, "-N", "")
	}
	return args
}

// path resolves the private key path for opts against the scanner's directory.
func (s *DiskScanner) genPath(opts GenerateOpts) (string, error) {
	dir := opts.Dir
	if dir == "" {
		var err error
		if dir, err = s.resolveDir(); err != nil {
			return "", err
		}
	}
	if opts.Name == "" {
		return "", fmt.Errorf("key name is required")
	}
	return filepath.Join(dir, opts.Name), nil
}

// Generate creates a key pair with ssh-keygen and no passphrase, returning the
// new identity. For interactive use (passphrase prompt) prefer GenerateCommand
// with tea.ExecProcess. Refuses to overwrite an existing key.
func (s *DiskScanner) Generate(opts GenerateOpts) (config.Identity, error) {
	privPath, err := s.genPath(opts)
	if err != nil {
		return config.Identity{}, err
	}
	if _, err := os.Stat(privPath); err == nil {
		return config.Identity{}, fmt.Errorf("key already exists: %s", privPath)
	}
	cmd := exec.Command("ssh-keygen", keygenArgs(opts, privPath, true)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return config.Identity{}, fmt.Errorf("ssh-keygen: %s: %w", out, err)
	}
	return config.Identity{
		ID:            config.IdentityID(opts.Name),
		Name:          opts.Name,
		Path:          privPath,
		PublicKeyPath: privPath + ".pub",
		Algorithm:     opts.Algorithm,
		Comment:       opts.Comment,
		ExistsOnDisk:  true,
	}, nil
}

// GenerateCommand builds an interactive ssh-keygen command (no -N), so
// tea.ExecProcess can yield the terminal for a passphrase prompt. privPath is
// resolved against dir (or ~/.ssh when empty).
func GenerateCommand(opts GenerateOpts) (*exec.Cmd, string, error) {
	s := &DiskScanner{Dir: opts.Dir}
	privPath, err := s.genPath(opts)
	if err != nil {
		return nil, "", err
	}
	return exec.Command("ssh-keygen", keygenArgs(opts, privPath, false)...), privPath, nil
}

// Delete removes a key pair: the private key at privPath and its .pub sibling.
// A missing file is not an error; an empty path is refused as a safety guard.
func (s *DiskScanner) Delete(privPath string) error {
	if privPath == "" {
		return fmt.Errorf("refusing to delete: empty key path")
	}
	if err := os.Remove(privPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", privPath, err)
	}
	pub := privPath + ".pub"
	if err := os.Remove(pub); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", pub, err)
	}
	return nil
}
