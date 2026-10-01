// Package sshconfig parses ~/.ssh/config (following Include directives) into
// the domain model and writes edits back while preserving comments, ordering,
// and unknown options via a round-tripping AST.
package sshconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	sshcfg "github.com/kevinburke/ssh_config"
	"github.com/s-johri/sshush/internal/config"
)

// ErrNotImplemented is returned by stubbed methods during scaffolding.
var ErrNotImplemented = errors.New("not implemented")

// ErrMatchReadOnly is returned when a mutation targets a `Match` block. Match
// conditions depend on connection-time state, so sshush surfaces them read-only.
var ErrMatchReadOnly = errors.New("Match blocks are read-only")

// maxIncludeDepth caps Include recursion, matching OpenSSH's limit.
const maxIncludeDepth = 5

// ConfigRepo loads and mutates SSH configuration. Mutations operate on the
// in-memory AST; Save backs up the file then writes the AST.
type ConfigRepo interface {
	Load() (*config.SshConfigModel, error)
	SetHostField(h config.HostID, key, val string) error
	DeleteHostField(h config.HostID, key string) error
	AddHostIdentity(h config.HostID, path string) error
	RemoveHostIdentity(h config.HostID, id config.IdentityID) error
	AddHost(config.Host) error
	DeleteHost(config.HostID) error
	Save() error
	Backups() []config.Backup
	RewriteCheck(h config.HostID) config.RewriteCheck
	Restore() ([]string, error)
}

// loadedFile is one parsed config file plus the bytes it was parsed from, so
// round-trip fidelity can be checked and writes target the right file.
type loadedFile struct {
	path string
	raw  []byte
	cfg  *sshcfg.Config
	// order is the chain of Include line numbers that led to this file (empty
	// for the main file). With hostLines it puts the blocks of all files in
	// the order OpenSSH reads them.
	order []int
	// hostLines are the line numbers of the Host and Match lines, one per
	// non-implicit block in cfg.Hosts.
	hostLines []int
	// opaque maps each placeholder line to the original line that it stands
	// for (see maskLines). Save writes the original back.
	opaque map[string]string
	// check is what the first save of this file changes besides the edit,
	// from comparing raw with render() at load (see RewriteCheck).
	check config.RewriteCheck
}

// FileRepo is a ConfigRepo backed by an on-disk config file plus its Includes.
type FileRepo struct {
	Path string // path to user config; empty means ~/.ssh/config
	// SshDir is the directory that relative Include directives and ~ resolve
	// against (per OpenSSH, ~/.ssh). Empty means ~/.ssh.
	SshDir string
	// Check, when set, is run by Save on a temp copy of each file before it
	// writes it (see SSHCheck). Save refuses a write when the current file
	// passes and the new one fails. Nil means no check.
	Check func(path string) error
	// BackupDir is where Save writes backups. Empty means DefaultBackupDir().
	// Backups must not go next to the config file: an Include glob such as
	// config.d/* would match them, and ssh would read the old values.
	BackupDir string

	files []*loadedFile   // parse order: main file first, then includes
	dirty map[string]bool // files mutated since load, keyed by path
	// backedUp marks files whose backup has been written. It must survive
	// reloads (the service reloads after every Save; resetting it would let
	// the next edit clobber the backup with already-edited content). Load
	// clears an entry only when the file changed outside sshush, so the next
	// Save re-snapshots the external state before writing over it.
	backedUp map[string]bool
	// warnings collects non-fatal problems found by the current Load.
	warnings []string
	// loadErr is the first file that did not load in the current Load. While
	// it is set, every change and Save is refused: the model is partial, and
	// a write could drop what did not load. Restore still works.
	loadErr error
}

// New returns a FileRepo for path. Empty path defaults to ~/.ssh/config.
func New(path string) *FileRepo {
	return &FileRepo{Path: path, backedUp: map[string]bool{}}
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

	// Remember each file's last-known bytes (kept current by Save) so a
	// reload can tell our own writes apart from external edits.
	prevRaw := map[string][]byte{}
	for _, lf := range r.files {
		prevRaw[lf.path] = lf.raw
	}

	r.files = nil
	r.dirty = map[string]bool{}
	r.warnings = nil
	r.loadErr = nil
	visited := map[string]bool{}
	if err := r.loadFile(main, nil, visited); err != nil {
		return nil, err
	}

	// A file that changed outside sshush (editor, Restore) makes the backup
	// stale: re-arm the backup so the next Save snapshots the new state
	// instead of letting Restore silently revert the external edits.
	for _, lf := range r.files {
		if prev, ok := prevRaw[lf.path]; ok && !bytes.Equal(prev, lf.raw) {
			delete(r.backedUp, lf.path)
		}
	}

	model := &config.SshConfigModel{
		Identities: map[config.IdentityID]config.Identity{},
		Hosts:      map[config.HostID]config.Host{},
		Warnings:   r.warnings,
	}
	if r.loadErr != nil {
		model.ConfigErr = r.loadErr.Error()
	}
	for _, lf := range r.files {
		model.SourceFiles = append(model.SourceFiles, lf.path)
	}
	// ssh uses the first block for an alias, so the model shows that one and
	// counts the others.
	for _, b := range r.blocks() {
		if first, ok := model.Hosts[b.model.ID]; ok {
			first.Duplicates++
			model.Hosts[b.model.ID] = first
			continue
		}
		model.Hosts[b.model.ID] = b.model
	}
	return model, nil
}

// loadFile parses one file, records it, then recurses into its Includes.
// order is the chain of Include line numbers that led here.
func (r *FileRepo) loadFile(path string, order []int, visited map[string]bool) error {
	if len(order) > maxIncludeDepth {
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
		r.failLoad(err) // keep going: show what does load
		return nil
	}

	masked, opaque := maskLines(raw)
	cfg, err := sshcfg.DecodeBytes(masked)
	var hostLines []int
	if err != nil {
		// Record the file with no blocks, so that Restore can still find
		// its backup. Its Includes are still followed.
		r.failLoad(fmt.Errorf("%s: %w", path, err))
		cfg, _ = sshcfg.DecodeBytes(nil)
		opaque = nil
	}
	lines, includes := scanDirectives(raw)
	if err == nil {
		hostLines = lines
	}
	lf := &loadedFile{path: path, raw: raw, cfg: cfg, order: order, hostLines: hostLines, opaque: opaque}
	lf.check = config.RewriteCheck{File: path}
	if err == nil {
		lf.check = checkRewrite(path, raw, lf.render())
	}
	r.files = append(r.files, lf)

	for _, inc := range includes {
		sub := append(append([]int(nil), order...), inc.line)
		for _, arg := range inc.args {
			for _, target := range r.resolveInclude(arg) {
				if err := r.loadFile(target, sub, visited); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// hostFromAST converts a parsed Host block into a model Host. Returns false for
// blocks with no patterns and for the parser's implicit global block (which
// holds directives before the first Host and shares the "*" pattern with an
// explicit "Host *"). Explicit wildcard blocks are surfaced with IsPattern set;
// `Match` blocks are surfaced read-only with IsMatch + MatchCriteria set.
func hostFromAST(h *sshcfg.Host) (config.Host, bool) {
	if isImplicitHost(h) {
		return config.Host{}, false
	}
	var names []string
	isPattern := false
	for _, p := range h.Patterns {
		s := p.String()
		if strings.ContainsAny(s, "*?!") {
			isPattern = true
		}
		names = append(names, s)
	}
	if len(names) == 0 {
		return config.Host{}, false
	}
	name := strings.Join(names, " ")

	host := config.Host{
		ID:        config.HostID(name),
		Name:      name,
		IsPattern: isPattern,
		Options:   map[string]string{},
	}
	// A Match block is keyed and labeled by its criteria, not its patterns, so
	// it can't be confused with (or collide with) a Host of the same name.
	if isMatchHost(h) {
		crit := matchCriteria(h)
		host.ID = config.HostID(crit)
		host.Name = crit
		host.IsMatch = true
		host.MatchCriteria = crit
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
			host.IdentityFiles = append(host.IdentityFiles, strings.Trim(val, `"`))
		default:
			host.Options[kv.Key] = val
		}
	}
	return host, true
}

// matchCriteria renders a Match block's condition for display, reconstructing
// the source line: "Match all" or "Match Host <patterns>". The library parses
// only `all` and `Host`; other criteria fail to parse upstream.
func matchCriteria(h *sshcfg.Host) string {
	kw := matchKeywordOf(h)
	if strings.EqualFold(kw, "all") {
		return "Match all"
	}
	pats := make([]string, 0, len(h.Patterns))
	for _, p := range h.Patterns {
		pats = append(pats, p.String())
	}
	return strings.TrimSpace("Match " + kw + " " + strings.Join(pats, " "))
}

// identityIDFromPath derives an IdentityID from an IdentityFile value by taking
// the file's base name (stem of .pub stripped), matching keys.DiskScanner IDs.
func identityIDFromPath(p string) config.IdentityID {
	p = strings.Trim(p, `"`)
	base := filepath.Base(p)
	base = strings.TrimSuffix(base, ".pub")
	return config.IdentityID(base)
}

// failLoad records the first load error of the current Load.
func (r *FileRepo) failLoad(err error) {
	if r.loadErr == nil {
		r.loadErr = err
	}
}

// writable returns an error when the last Load failed, so no change can be
// made to (or saved from) a partial model.
func (r *FileRepo) writable() error {
	if r.loadErr != nil {
		return fmt.Errorf("the SSH config did not load, so it cannot be changed: %w", r.loadErr)
	}
	return nil
}

// includeLine is one Include directive: its line number and its arguments.
type includeLine struct {
	line int
	args []string
}

// scanDirectives scans raw config bytes for the line numbers of Host and
// Match lines, and for Include directives. The ssh_config library resolves
// Includes into unexported state and keeps no block positions, so this is
// done here from the source lines. "Key=value" and indented lines count.
func scanDirectives(raw []byte) (hostLines []int, includes []includeLine) {
	for n, line := range strings.Split(string(raw), "\n") {
		key, args := splitDirective(line)
		switch key {
		case "host", "match":
			hostLines = append(hostLines, n)
		case "include":
			if len(args) > 0 {
				includes = append(includes, includeLine{line: n, args: args})
			}
		}
	}
	return hostLines, includes
}

// splitDirective returns the lower-case keyword of a config line and its
// arguments, up to a "#" comment. The keyword ends at a space, a tab or "="
// (ssh_config(5)). A blank or comment line gives "".
func splitDirective(line string) (key string, args []string) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return "", nil
	}
	rest := ""
	if i := strings.IndexAny(t, " \t="); i >= 0 {
		t, rest = t[:i], t[i:]
	}
	rest = strings.TrimPrefix(strings.TrimLeft(rest, " \t"), "=")
	for _, f := range strings.Fields(rest) {
		if strings.HasPrefix(f, "#") {
			break
		}
		args = append(args, f)
	}
	return strings.ToLower(t), args
}

// matchCriteriaWords are the Match criteria in ssh_config(5).
var matchCriteriaWords = map[string]bool{
	"all": true, "canonical": true, "final": true, "exec": true, "localnetwork": true,
	"host": true, "originalhost": true, "tagged": true, "command": true, "user": true,
	"localuser": true, "version": true, "sessiontype": true,
}

// parserSupportsMatch reports whether the ssh_config parser reads a Match
// line with these arguments correctly: only "all" alone, or "host" and a
// pattern list. It rejects other criteria, and it misreads "host x user bob"
// (user and bob become host patterns).
func parserSupportsMatch(args []string) bool {
	if len(args) == 1 && strings.EqualFold(args[0], "all") {
		return true
	}
	if len(args) < 2 || !strings.EqualFold(args[0], "host") {
		return false
	}
	for _, a := range args[1:] {
		if matchCriteriaWords[strings.ToLower(strings.TrimPrefix(a, "!"))] {
			return false
		}
	}
	return true
}

// opaqueMarker matches the placeholder that maskLines writes.
var opaqueMarker = regexp.MustCompile(`__sshush_opaque_\d+__`)

// maskLines replaces the lines that the parser must not see with
// placeholders, so the file still loads:
//
//   - A Match line that the parser cannot read becomes
//     "Match host __sshush_opaque_N__". The directives in the block stay as
//     they are, and the block stays a block of its own, so a new directive
//     for the block before it does not go into it.
//   - An Include line becomes the comment "# __sshush_opaque_N__". The
//     parser would read the included files itself (and fail on them); sshush
//     follows Includes with its own code (see loadFile).
//
// It returns the masked bytes and a map from each placeholder to the
// original line.
func maskLines(raw []byte) ([]byte, map[string]string) {
	lines := strings.Split(string(raw), "\n")
	var opaque map[string]string
	for i, line := range lines {
		key, args := splitDirective(line)
		var masked string
		switch {
		case key == "match" && !parserSupportsMatch(args):
			masked = "Match host "
		case key == "include":
			masked = "# "
		default:
			continue
		}
		if opaque == nil {
			opaque = map[string]string{}
		}
		marker := fmt.Sprintf("__sshush_opaque_%d__", len(opaque))
		opaque[marker] = line
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = indent + masked + marker
		if strings.HasSuffix(line, "\r") {
			lines[i] += "\r"
		}
	}
	if opaque == nil {
		return raw, nil
	}
	return []byte(strings.Join(lines, "\n")), opaque
}

// unmaskLines writes the original lines back in place of the placeholders,
// byte for byte.
func unmaskLines(data []byte, opaque map[string]string) []byte {
	if len(opaque) == 0 {
		return data
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if m := opaqueMarker.FindString(line); m != "" {
			if orig, ok := opaque[m]; ok {
				lines[i] = orig
			}
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// checkRewrite compares a file's bytes with what a save writes for it.
func checkRewrite(path string, raw, out []byte) config.RewriteCheck {
	c := config.RewriteCheck{File: path}
	if bytes.Equal(raw, out) {
		return c
	}
	c.Reformat = firstLineChange(string(raw), string(out))
	before, after := directiveList(string(raw)), directiveList(string(out))
	for i := 0; i < len(before) || i < len(after); i++ {
		var b, a []string
		if i < len(before) {
			b = before[i]
		}
		if i < len(after) {
			a = after[i]
		}
		if !slices.Equal(b, a) {
			c.Unsafe = fmt.Sprintf("%q would be read by ssh as %q", strings.Join(b, " "), strings.Join(a, " "))
			break
		}
	}
	return c
}

// firstLineChange describes the first line that differs between a and b.
func firstLineChange(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return fmt.Sprintf("line %d: %q becomes %q", i+1, x, y)
		}
	}
	return "the line endings change"
}

// directiveList returns the directives of a config text as OpenSSH reads
// them (see directiveTokens), without blank and comment lines.
func directiveList(text string) [][]string {
	var out [][]string
	for _, line := range strings.Split(text, "\n") {
		if d := directiveTokens(line); d != nil {
			out = append(out, d)
		}
	}
	return out
}

// directiveTokens splits one config line the way OpenSSH reads it: the
// keyword in lower case (it ends at a space, a tab or "="), then the
// arguments. Double quotes group an argument and are removed. An argument
// that starts with "#" outside quotes starts a comment, so "User u#x" is
// "u#x" but "User u #x" is "u". A blank or comment line gives nil.
func directiveTokens(line string) []string {
	t := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if t == "" || strings.HasPrefix(t, "#") {
		return nil
	}
	key, rest := t, ""
	if i := strings.IndexAny(t, " \t="); i >= 0 {
		key, rest = t[:i], t[i:]
	}
	rest = strings.TrimLeft(rest, " \t")
	rest = strings.TrimLeft(strings.TrimPrefix(rest, "="), " \t")
	out := []string{strings.ToLower(key)}
	var cur strings.Builder
	inQuote, inToken := false, false
	for _, r := range rest {
		switch {
		case r == '"':
			inQuote = !inQuote
			inToken = true
		case !inQuote && (r == ' ' || r == '\t'):
			if inToken {
				out = append(out, cur.String())
				cur.Reset()
				inToken = false
			}
		case !inQuote && !inToken && r == '#':
			return out
		default:
			cur.WriteRune(r)
			inToken = true
		}
	}
	if inToken {
		out = append(out, cur.String())
	}
	return out
}

// RewriteCheck reports what the first save of the file that holds host h
// changes besides the edit. An empty h means the main config file (where
// AddHost writes). See config.RewriteCheck.
func (r *FileRepo) RewriteCheck(h config.HostID) config.RewriteCheck {
	if h == "" {
		if len(r.files) > 0 {
			return r.files[0].check
		}
		return config.RewriteCheck{}
	}
	lf, _ := r.findHost(h)
	if lf == nil {
		return config.RewriteCheck{}
	}
	return lf.check
}

// render returns the bytes that Save writes for lf: the AST, with the
// original lines back in place of the placeholders.
func (lf *loadedFile) render() []byte {
	return unmaskLines([]byte(lf.cfg.String()), lf.opaque)
}

// opaqueCriteria returns the display criteria ("Match user bob") of a
// placeholder Match block, or "" when crit is not a placeholder.
func (lf *loadedFile) opaqueCriteria(crit string) string {
	m := opaqueMarker.FindString(crit)
	if m == "" {
		return ""
	}
	orig, ok := lf.opaque[m]
	if !ok {
		return ""
	}
	_, args := splitDirective(orig)
	return "Match " + strings.Join(args, " ")
}

// block is one host block with its place in OpenSSH's read order.
type block struct {
	lf    *loadedFile
	host  *sshcfg.Host
	model config.Host
	key   []int // Include line chain, then the block's own line
}

// blocks returns every surfaced host block in the order OpenSSH reads them:
// an Include's blocks come at the Include line. For an alias in more than
// one block, the first one in this list is the one ssh uses.
func (r *FileRepo) blocks() []block {
	var out []block
	for _, lf := range r.files {
		var hosts []*sshcfg.Host
		for _, h := range lf.cfg.Hosts {
			if !isImplicitHost(h) {
				hosts = append(hosts, h)
			}
		}
		// If the line scan disagrees with the parser, fall back to the block
		// index: the order inside the file stays right.
		lines := lf.hostLines
		if len(lines) != len(hosts) {
			lines = make([]int, len(hosts))
			for i := range lines {
				lines[i] = i
			}
		}
		for i, h := range hosts {
			mh, ok := hostFromAST(h)
			if !ok {
				continue // wildcard-only / empty block: nothing to surface
			}
			if crit := lf.opaqueCriteria(mh.MatchCriteria); crit != "" {
				mh.ID, mh.Name, mh.MatchCriteria = config.HostID(crit), crit, crit
			}
			key := append(append([]int(nil), lf.order...), lines[i])
			out = append(out, block{lf: lf, host: h, model: mh, key: key})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return slices.Compare(out[i].key, out[j].key) < 0 })
	return out
}

// resolveInclude expands ~ and globs and resolves relative paths against ~/.ssh,
// returning matched file paths. A glob match that ends in ".bak" is skipped
// with a warning: it is an old sshush backup, and ssh still reads it.
func (r *FileRepo) resolveInclude(arg string) []string {
	arg = strings.Trim(arg, `"`)
	if strings.HasPrefix(arg, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			arg = filepath.Join(home, arg[2:])
		}
	}
	if !filepath.IsAbs(arg) {
		arg = filepath.Join(r.sshBaseDir(), arg)
	}
	matches, err := filepath.Glob(arg)
	if err != nil || matches == nil {
		return nil
	}
	if strings.HasSuffix(arg, ".bak") {
		return matches // the user asked for this .bak by name
	}
	out := matches[:0]
	for _, m := range matches {
		if strings.HasSuffix(m, ".bak") {
			r.warnings = append(r.warnings, fmt.Sprintf(
				"ssh reads the old backup %s through an Include. Remove this file.", m))
			continue
		}
		out = append(out, m)
	}
	return out
}

// sshBaseDir is the directory relative Includes resolve against: r.SshDir, else
// ~/.ssh.
func (r *FileRepo) sshBaseDir() string {
	if r.SshDir != "" {
		return r.SshDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ssh"
	}
	return filepath.Join(home, ".ssh")
}

// resolvePath returns r.Path, else <SshDir>/config (SshDir defaulting to ~/.ssh).
func (r *FileRepo) resolvePath() (string, error) {
	if r.Path != "" {
		return r.Path, nil
	}
	return filepath.Join(r.sshBaseDir(), "config"), nil
}

// SetHostField sets key=val on host h, updating the existing directive in place
// (preserving its indentation and trailing comment) or appending a new one that
// mimics the block's indentation. The change is in-memory only until Save.
func (r *FileRepo) SetHostField(h config.HostID, key, val string) error {
	if err := r.writable(); err != nil {
		return err
	}
	if err := config.ValidateValue(key, val); err != nil {
		return err
	}
	lf, host := r.findHost(h)
	if host == nil {
		return fmt.Errorf("unknown host %q", h)
	}
	if isMatchHost(host) {
		return ErrMatchReadOnly
	}

	for _, node := range host.Nodes {
		kv, ok := node.(*sshcfg.KV)
		if ok && strings.EqualFold(kv.Key, key) {
			setKVValue(kv, val)
			r.dirty[lf.path] = true
			return nil
		}
	}

	// No existing directive: append one indented like its siblings. Only a
	// new name is checked: one already in the file is ssh's (or the user's)
	// business, for example a newer option or one under IgnoreUnknown.
	if err := config.ValidateOption(key); err != nil {
		return err
	}
	kv := &sshcfg.KV{Key: key, Value: val}
	setKVIndent(kv, blockIndent(host))
	host.Nodes = append(host.Nodes, kv)
	r.dirty[lf.path] = true
	return nil
}

// DeleteHostField removes the first directive matching key from host h. It is a
// no-op (no error) if the host has no such directive. In-memory until Save.
func (r *FileRepo) DeleteHostField(h config.HostID, key string) error {
	if err := r.writable(); err != nil {
		return err
	}
	lf, host := r.findHost(h)
	if host == nil {
		return fmt.Errorf("unknown host %q", h)
	}
	if isMatchHost(host) {
		return ErrMatchReadOnly
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

// AddHostIdentity appends an IdentityFile directive to host h (IdentityFile may
// appear multiple times). In-memory until Save.
func (r *FileRepo) AddHostIdentity(h config.HostID, path string) error {
	if err := r.writable(); err != nil {
		return err
	}
	if err := config.ValidateValue("IdentityFile", path); err != nil {
		return err
	}
	lf, host := r.findHost(h)
	if host == nil {
		return fmt.Errorf("unknown host %q", h)
	}
	if isMatchHost(host) {
		return ErrMatchReadOnly
	}
	// Skip if this exact path is already associated.
	for _, node := range host.Nodes {
		if kv, ok := node.(*sshcfg.KV); ok &&
			strings.EqualFold(kv.Key, "IdentityFile") && strings.Trim(kv.Value, `"`) == path {
			return nil
		}
	}
	kv := &sshcfg.KV{Key: "IdentityFile", Value: path}
	setKVIndent(kv, blockIndent(host))
	host.Nodes = append(host.Nodes, kv)
	r.dirty[lf.path] = true
	return nil
}

// RemoveHostIdentity removes the IdentityFile directive whose path resolves to
// identity id. No-op if not found.
func (r *FileRepo) RemoveHostIdentity(h config.HostID, id config.IdentityID) error {
	if err := r.writable(); err != nil {
		return err
	}
	lf, host := r.findHost(h)
	if host == nil {
		return fmt.Errorf("unknown host %q", h)
	}
	if isMatchHost(host) {
		return ErrMatchReadOnly
	}
	for i, node := range host.Nodes {
		kv, ok := node.(*sshcfg.KV)
		if ok && strings.EqualFold(kv.Key, "IdentityFile") && identityIDFromPath(kv.Value) == id {
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
	if err := r.writable(); err != nil {
		return err
	}
	if err := validateNewHost(h); err != nil {
		return err
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
	for _, k := range sortedKeys(h.Options) { // deterministic option order
		host.Nodes = append(host.Nodes, newKV(k, h.Options[k]))
	}
	host.Nodes = append(host.Nodes, &sshcfg.Empty{}) // trailing blank line

	main.cfg.Hosts = append(main.cfg.Hosts, host)
	r.dirty[main.path] = true
	return nil
}

// DeleteHost removes a host block from whichever file defines it.
func (r *FileRepo) DeleteHost(h config.HostID) error {
	if err := r.writable(); err != nil {
		return err
	}
	lf, host := r.findHost(h)
	if host == nil {
		return fmt.Errorf("unknown host %q", h)
	}
	if isMatchHost(host) {
		return ErrMatchReadOnly
	}
	i := slices.Index(lf.cfg.Hosts, host)
	lf.cfg.Hosts = append(lf.cfg.Hosts[:i], lf.cfg.Hosts[i+1:]...)
	r.dirty[lf.path] = true
	return nil
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

// sortedKeys returns a map's keys in sorted order for deterministic output.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// newKV builds a host directive indented four spaces (config convention).
func newKV(key, val string) *sshcfg.KV {
	kv := &sshcfg.KV{Key: key, Value: val}
	setKVIndent(kv, 4)
	return kv
}

// Save writes every dirty file back to disk. Before the first write of a file
// this session it copies the file's original contents to backupPath(path), so
// an unexpected result is always recoverable. Files are written with their
// existing permissions (default 0600).
func (r *FileRepo) Save() error {
	if err := r.writable(); err != nil {
		return err
	}
	for _, lf := range r.files {
		if !r.dirty[lf.path] {
			continue
		}
		if lf.check.Unsafe != "" {
			return fmt.Errorf("not saved: writing %s would change a value: %s", lf.path, lf.check.Unsafe)
		}
		if err := r.sshAccepts(lf.raw, lf.render()); err != nil {
			return fmt.Errorf("not saved: ssh rejects the new %s: %w", lf.path, err)
		}
		if !r.backupOnDisk(lf.path) {
			bak := r.backupPath(lf.path)
			if err := os.MkdirAll(filepath.Dir(bak), 0o700); err != nil {
				return fmt.Errorf("backup %s: %w", lf.path, err)
			}
			if err := os.WriteFile(bak, lf.raw, 0o600); err != nil {
				return fmt.Errorf("backup %s: %w", lf.path, err)
			}
			r.backedUp[lf.path] = true
		}
		data := lf.render()
		if err := os.WriteFile(lf.path, data, fileMode(lf.path)); err != nil {
			return fmt.Errorf("write %s: %w", lf.path, err)
		}
		lf.raw = data // keep last-known bytes current so reloads can spot external edits
		r.dirty[lf.path] = false
		lf.check = config.RewriteCheck{File: lf.path} // the file is now in sshush's format
	}
	return nil
}

// validateNewHost checks the fields of a host that AddHost writes.
func validateNewHost(h config.Host) error {
	if err := config.ValidateAlias(h.Name); err != nil {
		return err
	}
	if h.Hostname != "" {
		if err := config.ValidateValue("HostName", h.Hostname); err != nil {
			return err
		}
	}
	if h.User != "" {
		if err := config.ValidateValue("User", h.User); err != nil {
			return err
		}
	}
	if h.Port != 0 {
		if err := config.ValidateValue("Port", strconv.Itoa(h.Port)); err != nil {
			return err
		}
	}
	for _, k := range sortedKeys(h.Options) {
		if err := config.ValidateOption(k); err != nil {
			return err
		}
		if err := config.ValidateValue(k, h.Options[k]); err != nil {
			return err
		}
	}
	return nil
}

// sshAccepts runs r.Check on temp copies of a file's current and new
// content. It returns an error only when the current content passes and the
// new content fails, so a config that the local ssh already rejects (for
// example a newer option) does not block every save. It does not run for a
// file with a Match exec block, because ssh -G would run its command.
func (r *FileRepo) sshAccepts(before, after []byte) error {
	if r.Check == nil || hasMatchExec(before) || hasMatchExec(after) {
		return nil
	}
	if checkContent(r.Check, before) != nil {
		return nil
	}
	return checkContent(r.Check, after)
}

// checkContent writes data to a private temp file and runs check on it.
func checkContent(check func(string) error, data []byte) error {
	f, err := os.CreateTemp("", "sshush-check-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := check(f.Name()); err != nil {
		// The message names the temp file; name what it stands for.
		return errors.New(strings.ReplaceAll(err.Error(), f.Name(), "the new content"))
	}
	return nil
}

// hasMatchExec reports whether a config text has a Match block with an exec
// criterion.
func hasMatchExec(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		d := directiveTokens(line)
		if len(d) == 0 || d[0] != "match" {
			continue
		}
		for _, a := range d[1:] {
			if strings.EqualFold(strings.TrimPrefix(a, "!"), "exec") {
				return true
			}
		}
	}
	return false
}

// SSHCheck runs "ssh -G -F path" to check that ssh accepts a config file. ssh
// reads every line of the file, also in blocks that do not match, so a bad
// option name or value anywhere fails. It returns nil when ssh is not
// installed.
func SSHCheck(path string) error {
	bin, err := exec.LookPath("ssh")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-G", "-F", path, "sshush-config-check").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

// DefaultBackupDir is $XDG_STATE_HOME/sshush/backups, or
// ~/.local/state/sshush/backups when XDG_STATE_HOME is not set.
func DefaultBackupDir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "sshush", "backups")
}

// backupPath is where Save writes the backup of the config file at path: the
// absolute path, escaped into one file name, in the backup dir.
func (r *FileRepo) backupPath(path string) string {
	dir := r.BackupDir
	if dir == "" {
		dir = DefaultBackupDir()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return filepath.Join(dir, url.PathEscape(abs)+".bak")
}

// findBackup returns the backup to restore path from: the one in the backup
// dir, else a sibling "<path>.bak" that sshush wrote before 1.0. It returns
// "" when there is none.
func (r *FileRepo) findBackup(path string) string {
	for _, bak := range []string{r.backupPath(path), path + ".bak"} {
		if _, err := os.Stat(bak); err == nil {
			return bak
		}
	}
	return ""
}

// backupOnDisk reports whether this session's backup for path was written and
// is still present. Checking the disk guards against the backup being deleted
// externally mid-session, which would otherwise leave later edits unrecoverable.
func (r *FileRepo) backupOnDisk(path string) bool {
	if !r.backedUp[path] {
		return false
	}
	_, err := os.Stat(r.backupPath(path))
	return err == nil
}

// Backups returns the backup of each loaded config file that has one. A
// backup is written before sshush's first edit of a file in a session, so it
// can be from an earlier session: ModTime tells the user how old it is.
func (r *FileRepo) Backups() []config.Backup {
	var out []config.Backup
	for _, lf := range r.files {
		bak := r.findBackup(lf.path)
		if bak == "" {
			continue
		}
		var mod time.Time
		if fi, err := os.Stat(bak); err == nil {
			mod = fi.ModTime()
		}
		out = append(out, config.Backup{
			File:       lf.path,
			Path:       bak,
			ModTime:    mod,
			PreRestore: r.preRestorePath(lf.path),
		})
	}
	return out
}

// preRestorePath is where Restore saves the current content of path before it
// overwrites it. It is always in the backup dir, also for a legacy sibling
// .bak, so that an Include glob cannot match it.
func (r *FileRepo) preRestorePath(path string) string {
	return r.backupPath(path) + ".pre-restore"
}

// Restore overwrites each loaded file that has a backup with the backup's
// contents, reverting every change made since the backup was taken (sshush's
// first edit of the file in some session, see Backups). Before each overwrite
// it saves the current content to the PreRestore path. Returns the restored
// file paths. The in-memory AST
// is left stale on purpose — callers reload (via Refresh) to pick up the
// reverted content.
func (r *FileRepo) Restore() ([]string, error) {
	var restored []string
	for _, lf := range r.files {
		bak := r.findBackup(lf.path)
		if bak == "" {
			continue
		}
		data, err := os.ReadFile(bak)
		if err != nil {
			return restored, fmt.Errorf("read backup %s: %w", bak, err)
		}
		if err := r.savePreRestore(lf.path); err != nil {
			return restored, err
		}
		if err := os.WriteFile(lf.path, data, fileMode(lf.path)); err != nil {
			return restored, fmt.Errorf("restore %s: %w", lf.path, err)
		}
		restored = append(restored, lf.path)
	}
	return restored, nil
}

// findHost locates the file and AST host block for a model HostID: the first
// block for that alias in OpenSSH order, which is the one the model shows.
func (r *FileRepo) findHost(h config.HostID) (*loadedFile, *sshcfg.Host) {
	for _, b := range r.blocks() {
		if b.model.ID == h {
			return b.lf, b.host
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

// savePreRestore copies the current content of path to preRestorePath(path),
// so that the restore can be undone. A missing file has nothing to save.
func (r *FileRepo) savePreRestore(path string) error {
	cur, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	pre := r.preRestorePath(path)
	if err := os.MkdirAll(filepath.Dir(pre), 0o700); err != nil {
		return fmt.Errorf("save %s before restore: %w", path, err)
	}
	if err := os.WriteFile(pre, cur, 0o600); err != nil {
		return fmt.Errorf("save %s before restore: %w", path, err)
	}
	return nil
}
