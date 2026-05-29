// Package sshconfig parses ~/.ssh/config (following Include directives) into
// the domain model and writes edits back while preserving comments, ordering,
// and unknown options via a round-tripping AST.
package sshconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	sshcfg "github.com/kevinburke/ssh_config"
	"github.com/s-johri/sshush/pkg/config"
)

// ErrNotImplemented is returned by stubbed methods during scaffolding.
var ErrNotImplemented = errors.New("not implemented")

// maxIncludeDepth caps Include recursion, matching OpenSSH's limit.
const maxIncludeDepth = 5

// ConfigRepo loads and mutates SSH configuration. Mutations operate on the
// in-memory AST; Save backs up the file then writes the AST.
type ConfigRepo interface {
	Load() (*config.SshConfigModel, error)
	SetHostField(h config.HostID, key, val string) error
	AddHost(config.Host) error
	DeleteHost(config.HostID) error
	Save() error
}

// loadedFile is one parsed config file plus the bytes it was parsed from, so
// round-trip fidelity can be checked and writes target the right file.
type loadedFile struct {
	path string
	raw  []byte
	cfg  *sshcfg.Config
}

// FileRepo is a ConfigRepo backed by an on-disk config file plus its Includes.
type FileRepo struct {
	Path string // path to user config; empty means ~/.ssh/config

	files []*loadedFile // parse order: main file first, then includes
}

// New returns a FileRepo for path. Empty path defaults to ~/.ssh/config.
func New(path string) *FileRepo {
	return &FileRepo{Path: path}
}

// Load parses the user config and every file it Includes, building the unified
// model. A missing main config yields an empty model, not an error. Includes
// are resolved relative to ~/.ssh (per OpenSSH), with globbing and ~ expansion,
// deduped, and capped at maxIncludeDepth.
func (r *FileRepo) Load() (*config.SshConfigModel, error) {
	main, err := r.resolvePath()
	if err != nil {
		return nil, err
	}

	r.files = nil
	visited := map[string]bool{}
	if err := r.loadFile(main, 0, visited); err != nil {
		return nil, err
	}

	model := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{},
		Hosts:      map[config.HostID]config.Host{},
	}
	for _, lf := range r.files {
		model.SourceFiles = append(model.SourceFiles, lf.path)
		for _, h := range lf.cfg.Hosts {
			host, ok := hostFromAST(h)
			if !ok {
				continue // wildcard-only / empty block: nothing to surface
			}
			model.Hosts[host.ID] = host
		}
	}
	return model, nil
}

// loadFile parses one file, records it, then recurses into its Includes.
func (r *FileRepo) loadFile(path string, depth int, visited map[string]bool) error {
	if depth > maxIncludeDepth {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if visited[abs] {
		return nil // already parsed; avoid loops and duplicates
	}
	visited[abs] = true

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // missing file (incl. main) is not fatal
		}
		return err
	}

	cfg, err := sshcfg.DecodeBytes(raw)
	if err != nil {
		return err
	}
	r.files = append(r.files, &loadedFile{path: path, raw: raw, cfg: cfg})

	for _, inc := range includeTargets(raw) {
		for _, target := range resolveInclude(inc) {
			if err := r.loadFile(target, depth+1, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// hostFromAST converts a parsed Host block into a model Host. Returns false for
// blocks with no concrete (non-wildcard) pattern.
func hostFromAST(h *sshcfg.Host) (config.Host, bool) {
	var names []string
	for _, p := range h.Patterns {
		s := p.String()
		if s == "*" || strings.ContainsAny(s, "*?!") {
			continue
		}
		names = append(names, s)
	}
	if len(names) == 0 {
		return config.Host{}, false
	}
	name := strings.Join(names, " ")

	host := config.Host{
		ID:      config.HostID(name),
		Name:    name,
		Options: map[string]string{},
	}
	for _, node := range h.Nodes {
		kv, ok := node.(*sshcfg.KV)
		if !ok {
			continue
		}
		val := strings.TrimSpace(kv.Value)
		switch {
		case strings.EqualFold(kv.Key, "HostName"):
			host.Hostname = val
		case strings.EqualFold(kv.Key, "User"):
			host.User = val
		case strings.EqualFold(kv.Key, "Port"):
			host.Port, _ = strconv.Atoi(val)
		case strings.EqualFold(kv.Key, "IdentityFile"):
			id := identityIDFromPath(val)
			host.Identities = append(host.Identities, id)
		default:
			host.Options[kv.Key] = val
		}
	}
	return host, true
}

// identityIDFromPath derives an IdentityID from an IdentityFile value by taking
// the file's base name (stem of .pub stripped), matching keys.DiskScanner IDs.
func identityIDFromPath(p string) config.IdentityID {
	p = strings.Trim(p, `"`)
	base := filepath.Base(p)
	base = strings.TrimSuffix(base, ".pub")
	return config.IdentityID(base)
}

// includeTargets scans raw config bytes for Include directive arguments. The
// ssh_config library resolves Includes into unexported state, so enumeration is
// done here from the source lines.
func includeTargets(raw []byte) []string {
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		fields := strings.Fields(t)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "Include") {
			continue
		}
		out = append(out, fields[1:]...)
	}
	return out
}

// resolveInclude expands ~ and globs and resolves relative paths against ~/.ssh,
// returning matched file paths.
func resolveInclude(arg string) []string {
	arg = strings.Trim(arg, `"`)
	if strings.HasPrefix(arg, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			arg = filepath.Join(home, arg[2:])
		}
	}
	if !filepath.IsAbs(arg) {
		if home, err := os.UserHomeDir(); err == nil {
			arg = filepath.Join(home, ".ssh", arg)
		}
	}
	matches, err := filepath.Glob(arg)
	if err != nil || matches == nil {
		return nil
	}
	return matches
}

// resolvePath returns r.Path or ~/.ssh/config when empty.
func (r *FileRepo) resolvePath() (string, error) {
	if r.Path != "" {
		return r.Path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// SetHostField implements ConfigRepo. (Milestone 7.)
func (r *FileRepo) SetHostField(h config.HostID, key, val string) error {
	return ErrNotImplemented
}

// AddHost implements ConfigRepo. (Milestone 8.)
func (r *FileRepo) AddHost(h config.Host) error {
	return ErrNotImplemented
}

// DeleteHost implements ConfigRepo. (Milestone 8.)
func (r *FileRepo) DeleteHost(h config.HostID) error {
	return ErrNotImplemented
}

// Save implements ConfigRepo. (Milestone 7: backup then write each file.)
func (r *FileRepo) Save() error {
	return ErrNotImplemented
}
