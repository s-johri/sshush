package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

// fakeAsset and fakeRelease implement the selfupdate source interfaces.
type fakeAsset struct {
	id   int64
	name string
	data []byte
}

func (a fakeAsset) GetID() int64                  { return a.id }
func (a fakeAsset) GetName() string               { return a.name }
func (a fakeAsset) GetSize() int                  { return len(a.data) }
func (a fakeAsset) GetBrowserDownloadURL() string { return "https://example.invalid/" + a.name }

type fakeRelease struct{ assets []fakeAsset }

func (r fakeRelease) GetID() int64              { return 1 }
func (r fakeRelease) GetTagName() string        { return "v9.9.9" }
func (r fakeRelease) GetDraft() bool            { return false }
func (r fakeRelease) GetPrerelease() bool       { return false }
func (r fakeRelease) GetPublishedAt() time.Time { return time.Time{} }
func (r fakeRelease) GetReleaseNotes() string   { return "" }
func (r fakeRelease) GetName() string           { return "v9.9.9" }
func (r fakeRelease) GetURL() string            { return "" }
func (r fakeRelease) GetAssets() []selfupdate.SourceAsset {
	out := make([]selfupdate.SourceAsset, len(r.assets))
	for i, a := range r.assets {
		out[i] = a
	}
	return out
}

// fakeSource serves one release, the way goreleaser publishes it.
type fakeSource struct{ rel fakeRelease }

func (s fakeSource) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	return []selfupdate.SourceRelease{s.rel}, nil
}

func (s fakeSource) DownloadReleaseAsset(_ context.Context, _ *selfupdate.Release, id int64) (io.ReadCloser, error) {
	for _, a := range s.rel.assets {
		if a.id == id {
			return io.NopCloser(bytes.NewReader(a.data)), nil
		}
	}
	return nil, fmt.Errorf("no asset %d", id)
}

// archive returns a goreleaser-style tar.gz that holds an "sshush" binary.
func archive(t *testing.T, binary string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "sshush", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(binary)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// release returns a source with the archive and, if sums is not empty, a
// checksums.txt with that content. sums can use %s for the real hash.
func release(t *testing.T, binary, sums string) (fakeSource, string) {
	t.Helper()
	name := fmt.Sprintf("sshush_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	data := archive(t, binary)
	assets := []fakeAsset{{id: 1, name: name, data: data}}
	if sums != "" {
		line := fmt.Sprintf(sums, fmt.Sprintf("%x", sha256.Sum256(data))) + "  " + name + "\n"
		assets = append(assets, fakeAsset{id: 2, name: checksumsFile, data: []byte(line)})
	}
	return fakeSource{rel: fakeRelease{assets: assets}}, name
}

// oldExe writes a fake installed binary and returns its path.
func oldExe(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "sshush")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func runUpdate(t *testing.T, src selfupdate.Source, exe string) error {
	t.Helper()
	up, err := newUpdater(src)
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyUpdate(context.Background(), up, "v1.0.0", exe, io.Discard)
	return err
}

func TestUpdateVerifiesChecksum(t *testing.T) {
	src, _ := release(t, "NEW", "%s")
	exe := oldExe(t)
	if err := runUpdate(t, src, exe); err != nil {
		t.Fatalf("update with a good checksum: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "NEW" {
		t.Errorf("binary = %q, want the new one", got)
	}
}

// TestUpdateRejectsBadChecksum: an archive that does not match
// checksums.txt must not replace the binary (I7).
func TestUpdateRejectsBadChecksum(t *testing.T) {
	src, _ := release(t, "EVIL", strings.Repeat("0", 64)+"%.0s")
	exe := oldExe(t)
	if err := runUpdate(t, src, exe); err == nil {
		t.Error("update with a bad checksum succeeded")
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD" {
		t.Errorf("binary = %q, want it unchanged", got)
	}
}

// TestUpdateRejectsMissingChecksums: a release without checksums.txt cannot
// be checked, so it must not be installed.
func TestUpdateRejectsMissingChecksums(t *testing.T) {
	src, _ := release(t, "NEW", "")
	exe := oldExe(t)
	if err := runUpdate(t, src, exe); err == nil {
		t.Error("update without checksums.txt succeeded")
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD" {
		t.Errorf("binary = %q, want it unchanged", got)
	}
}

// TestChecksumsFileMatchesGoreleaser: the name must be the one that
// goreleaser publishes, or every update fails.
func TestChecksumsFileMatchesGoreleaser(t *testing.T) {
	cfg, err := os.ReadFile("../../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("name_template: %q", checksumsFile); !strings.Contains(string(cfg), want) {
		t.Errorf(".goreleaser.yaml does not contain %s", want)
	}
}
