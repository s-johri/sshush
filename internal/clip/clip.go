// Package clip copies text to the system clipboard. On Linux it relies on an
// external tool (xclip / xsel / wl-clipboard); macOS/Windows use the native
// pasteboard. Available reports whether copying will work so callers can warn.
package clip

import "github.com/atotto/clipboard"

// write is overridable in tests.
var write = clipboard.WriteAll

// Write copies s to the clipboard.
func Write(s string) error { return write(s) }

// SetWriter overrides the clipboard backend (tests). Returns a restore func.
func SetWriter(f func(string) error) func() {
	old := write
	write = f
	return func() { write = old }
}

// Available reports whether a clipboard backend is usable.
func Available() bool { return !clipboard.Unsupported }
