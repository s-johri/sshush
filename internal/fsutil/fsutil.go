// Package fsutil writes files so that a crash or a full disk never leaves a
// half-written SSH config or settings file.
package fsutil

import (
	"errors"
	"os"
	"path/filepath"
)

// WriteFile replaces the content of path with data. It writes a temp file
// in the same directory, syncs it, and renames it over the file, so a reader
// sees the old content or the new content, never a part of it.
//
//   - A symlink is resolved first, so a dotfiles link stays a link and its
//     target gets the new content.
//   - The mode of an existing file is kept. A new file gets mode.
//   - When no temp file can be made in the directory (for example a
//     read-only ~/.ssh that holds a writable config), it writes in place, as
//     os.WriteFile does.
func WriteFile(path string, data []byte, mode os.FileMode) (err error) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return os.WriteFile(path, data, mode)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
