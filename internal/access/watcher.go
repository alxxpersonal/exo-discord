package access

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// --- Constants ---

const watcherDebounceWindow = 100 * time.Millisecond

// --- Types ---

// ReloadSource names the filesystem source that triggered a reload.
type ReloadSource string

const (
	ReloadSourceConfig ReloadSource = "config"
	ReloadSourceState  ReloadSource = "state"
)

// WatchEvent reports a debounced access reload trigger.
type WatchEvent struct {
	Source ReloadSource
}

// Watcher observes config and access-state filesystem changes.
type Watcher struct {
	watcher *fsnotify.Watcher

	configPath string
	configDir  string
	statePath  string
	stateDir   string
	stateRoot  string

	events chan WatchEvent
	errors chan error
	done   chan struct{}

	closeOnce sync.Once
}

// --- Constructors ---

// NewWatcher creates a watcher for one policy file and one access-state file.
func NewWatcher(policyPath string, statePath string) (*Watcher, error) {
	rawWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	configPath := filepath.Clean(policyPath)
	stateFilePath := filepath.Clean(statePath)
	watcher := &Watcher{
		watcher:    rawWatcher,
		configPath: configPath,
		configDir:  filepath.Dir(configPath),
		statePath:  stateFilePath,
		stateDir:   filepath.Dir(stateFilePath),
		stateRoot:  filepath.Dir(filepath.Dir(stateFilePath)),
		events:     make(chan WatchEvent, 1),
		errors:     make(chan error, 1),
		done:       make(chan struct{}),
	}

	if err := watcher.addInitialWatches(); err != nil {
		_ = rawWatcher.Close()
		return nil, err
	}

	go watcher.run()
	return watcher, nil
}

// --- Channels ---

// Events returns the debounced reload event stream.
func (w *Watcher) Events() <-chan WatchEvent {
	return w.events
}

// Errors returns non-fatal watcher warnings and errors.
func (w *Watcher) Errors() <-chan error {
	return w.errors
}

// --- Lifecycle ---

// Close shuts down the watcher. Close is safe to call concurrently.
func (w *Watcher) Close() error {
	var closeErr error
	w.closeOnce.Do(func() {
		closeErr = w.watcher.Close()
		<-w.done
	})
	return closeErr
}

// --- Run Loop ---

func (w *Watcher) run() {
	defer close(w.done)
	defer close(w.events)
	defer close(w.errors)

	var (
		timer         *time.Timer
		timerCh       <-chan time.Time
		pendingSource ReloadSource
	)

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			source, relevant, err := w.handleEvent(event)
			if err != nil {
				w.publishError(err)
			}
			if !relevant {
				continue
			}

			pendingSource = source
			if timer == nil {
				timer = time.NewTimer(watcherDebounceWindow)
				timerCh = timer.C
				continue
			}

			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(watcherDebounceWindow)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.publishError(fmt.Errorf("watch access files: %w", err))
		case <-timerCh:
			select {
			case w.events <- WatchEvent{Source: pendingSource}:
			default:
			}

			timerCh = nil
			timer = nil
			pendingSource = ""
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) (ReloadSource, bool, error) {
	if !hasRelevantOp(event.Op) {
		return "", false, nil
	}

	name := filepath.Clean(event.Name)
	switch name {
	case w.configPath:
		return ReloadSourceConfig, true, nil
	case w.statePath:
		return ReloadSourceState, true, nil
	case w.stateDir:
		err := w.handleStateDirEvent(event)
		return ReloadSourceState, true, err
	default:
		return "", false, nil
	}
}

func (w *Watcher) handleStateDirEvent(event fsnotify.Event) error {
	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		return fmt.Errorf("access state dir is unavailable: %s", w.stateDir)
	}
	if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
		return nil
	}

	info, err := os.Stat(w.stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat access state dir %s: %w", w.stateDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("access state dir path is not a directory: %s", w.stateDir)
	}
	if err := w.watcher.Add(w.stateDir); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
		return fmt.Errorf("watch access state dir %s: %w", w.stateDir, err)
	}
	return nil
}

func (w *Watcher) publishError(err error) {
	if err == nil {
		return
	}

	select {
	case w.errors <- err:
	default:
	}
}

// --- Watch Helpers ---

func (w *Watcher) addInitialWatches() error {
	targets := uniqueDirs([]string{w.configDir, w.stateDir, w.stateRoot})
	for _, target := range targets {
		if err := w.addWatch(target); err != nil {
			return err
		}
	}
	return nil
}

func (w *Watcher) addWatch(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat watch target %s: %w", path, err)
	}
	if !info.IsDir() {
		return nil
	}
	if err := w.watcher.Add(path); err != nil {
		return fmt.Errorf("watch target %s: %w", path, err)
	}
	return nil
}

func hasRelevantOp(op fsnotify.Op) bool {
	return op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0
}

func uniqueDirs(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result
}
