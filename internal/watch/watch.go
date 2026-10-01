// Package watch reports filesystem changes under a set of directories, coalesced
// into a single signal so a burst of editor writes triggers one reload. It is
// used to hot-reload the SSH config and key directory.
package watch

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Debounce is how long to wait after the last fs event before signalling, so a
// multi-write save (truncate + write + chmod) collapses into one reload.
const Debounce = 250 * time.Millisecond

// Watcher coalesces filesystem events under watched directories into a channel
// of empty signals.
type Watcher struct {
	fsw    *fsnotify.Watcher
	events chan struct{}

	mu      sync.Mutex
	watched map[string]bool
}

// New starts a Watcher. Call Watch to set the directories of interest and
// Events to receive coalesced change signals.
func New() (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:     fsw,
		events:  make(chan struct{}, 1),
		watched: map[string]bool{},
	}
	go w.loop()
	return w, nil
}

// Watch replaces the set of watched directories with dirs (deduplicated). Paths
// that cannot be watched are skipped; watching is best-effort.
func (w *Watcher) Watch(dirs []string) error {
	want := map[string]bool{}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		want[filepath.Clean(d)] = true
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	// Remove directories no longer wanted.
	for d := range w.watched {
		if !want[d] {
			_ = w.fsw.Remove(d)
			delete(w.watched, d)
		}
	}
	// Add new directories.
	for d := range want {
		if w.watched[d] {
			continue
		}
		if err := w.fsw.Add(d); err == nil {
			w.watched[d] = true
		}
	}
	return nil
}

// Events returns the channel that receives one signal per coalesced change.
func (w *Watcher) Events() <-chan struct{} { return w.events }

// Close stops watching and releases resources.
func (w *Watcher) Close() error { return w.fsw.Close() }

// loop debounces raw fsnotify events into single signals.
func (w *Watcher) loop() {
	var timer *time.Timer
	var fire <-chan time.Time
	for {
		select {
		case _, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if timer == nil {
				timer = time.NewTimer(Debounce)
			} else {
				timer.Reset(Debounce)
			}
			fire = timer.C
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		case <-fire:
			fire = nil
			select {
			case w.events <- struct{}{}:
			default: // a pending signal is already queued; coalesce
			}
		}
	}
}
