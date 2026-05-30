// Package sshconfig parses ~/.ssh/config (following Include directives) into
// the domain model and writes edits back while preserving comments, ordering,
// and unknown options via a round-tripping AST.
package sshconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"unsafe"

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
	DeleteHostField(h config.HostID, key string) error
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

	files    []*loadedFile   // parse order: main file first, then includes
	dirty    map[string]bool // files mutated since load, keyed by path
	backedUp map[string]bool // files whose .bak has been written this session
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
	r.dirty = map[string]bool{}
	r.backedUp = map[string]bool{}
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

// SetHostField sets key=val on host h, updating the existing directive in place
// (preserving its indentation and trailing comment) or appending a new one that
// mimics the block's indentation. The change is in-memory only until Save.
func (r *FileRepo) SetHostField(h config.HostID, key, val string) error {
	lf, host := r.findHost(h)
	if host == nil {
		return fmt.Errorf("unknown host %q", h)
	}

	for _, node := range host.Nodes {
		kv, ok := node.(*sshcfg.KV)
		if ok && strings.EqualFold(kv.Key, key) {
			setKVValue(kv, val)
			r.dirty[lf.path] = true
			return nil
		}
	}

	// No existing directive: append one indented like its siblings.
	kv := &sshcfg.KV{Key: key, Value: val}
	setKVIndent(kv, blockIndent(host))
	host.Nodes = append(host.Nodes, kv)
	r.dirty[lf.path] = true
	return nil
}

// DeleteHostField removes the first directive matching key from host h. It is a
// no-op (no error) if the host has no such directive. In-memory until Save.
func (r *FileRepo) DeleteHostField(h config.HostID, key string) error {
	lf, host := r.findHost(h)
	if host == nil {
		return fmt.Errorf("unknown host %q", h)
	}
	for i, node := range host.Nodes {
		if kv, ok := node.(*sshcfg.KV); ok && strings.EqualFold(kv.Key, key) {
			host.Nodes = append(host.Nodes[:i], host.Nodes[i+1:]...)
			r.dirty[lf.path] = true
			return nil
		}
	}
	return nil
}

// AddHost appends a new Host block to the main config file. The block carries
// the host's set fields (HostName/User/Port) and any Options, indented four
// spaces, followed by a blank line. In-memory until Save.
func (r *FileRepo) AddHost(h config.Host) error {
	if strings.TrimSpace(h.Name) == "" {
		return errors.New("host name is required")
	}
	main, err := r.ensureMainFile()
	if err != nil {
		return err
	}
	if _, existing := r.findHost(h.ID); existing != nil {
		return fmt.Errorf("host %q already exists", h.ID)
	}

	var pats []*sshcfg.Pattern
	for _, name := range strings.Fields(h.Name) {
		p, err := sshcfg.NewPattern(name)
		if err != nil {
			return fmt.Errorf("invalid host pattern %q: %w", name, err)
		}
		pats = append(pats, p)
	}

	host := &sshcfg.Host{Patterns: pats}
	if h.Hostname != "" {
		host.Nodes = append(host.Nodes, newKV("HostName", h.Hostname))
	}
	if h.User != "" {
		host.Nodes = append(host.Nodes, newKV("User", h.User))
	}
	if h.Port != 0 {
		host.Nodes = append(host.Nodes, newKV("Port", strconv.Itoa(h.Port)))
	}
	for k, v := range h.Options {
		host.Nodes = append(host.Nodes, newKV(k, v))
	}
	host.Nodes = append(host.Nodes, &sshcfg.Empty{}) // trailing blank line

	main.cfg.Hosts = append(main.cfg.Hosts, host)
	r.dirty[main.path] = true
	return nil
}

// DeleteHost removes a host block from whichever file defines it.
func (r *FileRepo) DeleteHost(h config.HostID) error {
	for _, lf := range r.files {
		for i, host := range lf.cfg.Hosts {
			if mh, ok := hostFromAST(host); ok && mh.ID == h {
				lf.cfg.Hosts = append(lf.cfg.Hosts[:i], lf.cfg.Hosts[i+1:]...)
				r.dirty[lf.path] = true
				return nil
			}
		}
	}
	return fmt.Errorf("unknown host %q", h)
}

// ensureMainFile returns the main loadedFile, creating an empty one (and its
// in-memory Config) if the config file did not exist at load time.
func (r *FileRepo) ensureMainFile() (*loadedFile, error) {
	if len(r.files) > 0 {
		return r.files[0], nil
	}
	path, err := r.resolvePath()
	if err != nil {
		return nil, err
	}
	cfg, err := sshcfg.DecodeBytes(nil)
	if err != nil {
		return nil, err
	}
	lf := &loadedFile{path: path, raw: nil, cfg: cfg}
	r.files = append(r.files, lf)
	return lf, nil
}

// newKV builds a host directive indented four spaces (config convention).
func newKV(key, val string) *sshcfg.KV {
	kv := &sshcfg.KV{Key: key, Value: val}
	setKVIndent(kv, 4)
	return kv
}

// Save writes every dirty file back to disk. Before the first write of a file
// this session it copies the file's original contents to "<path>.bak", so an
// unexpected result is always recoverable. Files are written with their
// existing permissions (default 0600).
func (r *FileRepo) Save() error {
	for _, lf := range r.files {
		if !r.dirty[lf.path] {
			continue
		}
		if !r.backedUp[lf.path] {
			if err := os.WriteFile(lf.path+".bak", lf.raw, fileMode(lf.path)); err != nil {
				return fmt.Errorf("backup %s: %w", lf.path, err)
			}
			r.backedUp[lf.path] = true
		}
		if err := os.WriteFile(lf.path, []byte(lf.cfg.String()), fileMode(lf.path)); err != nil {
			return fmt.Errorf("write %s: %w", lf.path, err)
		}
		r.dirty[lf.path] = false
	}
	return nil
}

// findHost locates the file and AST host block for a model HostID.
func (r *FileRepo) findHost(h config.HostID) (*loadedFile, *sshcfg.Host) {
	for _, lf := range r.files {
		for _, host := range lf.cfg.Hosts {
			if mh, ok := hostFromAST(host); ok && mh.ID == h {
				return lf, host
			}
		}
	}
	return nil, nil
}

// fileMode returns the file's current permissions, or 0600 if it can't stat.
func fileMode(path string) os.FileMode {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode().Perm()
	}
	return 0o600
}

// blockIndent returns the leading-space width of the first directive in a host
// block, so appended directives line up. Defaults to 4 spaces.
func blockIndent(host *sshcfg.Host) int {
	for _, node := range host.Nodes {
		if kv, ok := node.(*sshcfg.KV); ok {
			return int(reflect.ValueOf(kv).Elem().FieldByName("leadingSpace").Int())
		}
	}
	return 4
}

// setKVValue updates a KV's value so String() emits it. The library always
// populates the unexported rawValue from the parsed text and prefers it over
// Value, so rawValue must be cleared. Indentation, spacing, and any trailing
// comment are preserved. Pinned to ssh_config v1.6.0; guarded by TestSetField.
func setKVValue(kv *sshcfg.KV, val string) {
	kv.Value = val
	setUnexportedString(kv, "rawValue", "")
}

// setKVIndent sets a freshly created KV's leading indentation (unexported).
func setKVIndent(kv *sshcfg.KV, spaces int) {
	v := reflect.ValueOf(kv).Elem().FieldByName("leadingSpace")
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetInt(int64(spaces))
}

func setUnexportedString(ptr any, field, val string) {
	v := reflect.ValueOf(ptr).Elem().FieldByName(field)
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetString(val)
}
