package shellinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledDetection(t *testing.T) {
	dir := t.TempDir()
	bashrc := filepath.Join(dir, ".bashrc")
	zshrc := filepath.Join(dir, ".zshrc")
	orig := rcCandidates
	rcCandidates = func() []string { return []string{bashrc, zshrc} }
	defer func() { rcCandidates = orig }()

	// Nothing present.
	if files, any := Installed(); any || len(files) != 0 {
		t.Fatalf("expected none installed, got %v", files)
	}

	// A file without the marker is not detected.
	if err := os.WriteFile(zshrc, []byte("export PATH=$PATH\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, any := Installed(); any {
		t.Error("file without marker should not count")
	}

	// Install the snippet into .bashrc.
	if err := os.WriteFile(bashrc, []byte(Snippet), 0o644); err != nil {
		t.Fatal(err)
	}
	files, any := Installed()
	if !any || len(files) != 1 || files[0] != bashrc {
		t.Errorf("expected bashrc detected, got %v", files)
	}
}

func TestSnippetContainsMarker(t *testing.T) {
	if !strings.Contains(Snippet, marker) {
		t.Error("snippet must contain the detection marker")
	}
}
