package access

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// --- Test Cases ---

func TestWatcherDebouncesRapidConfigChanges(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	watcher, err := NewWatcher(fixture.configPath, fixture.statePath)
	if err != nil {
		t.Fatalf("NewWatcher() error = %v", err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	writeHotReloadConfig(t, fixture.configPath, hotReloadConfigBody(""))
	writeHotReloadConfig(t, fixture.configPath, hotReloadConfigBody("user-1"))

	event := waitForWatchEvent(t, watcher.Events())
	if event.Source != ReloadSourceConfig {
		t.Fatalf("event source = %q, want config", event.Source)
	}

	select {
	case extra := <-watcher.Events():
		t.Fatalf("extra event = %#v, want no second debounced event", extra)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestWatcherReportsConfigAndStateDirChanges(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	watcher, err := NewWatcher(fixture.configPath, fixture.statePath)
	if err != nil {
		t.Fatalf("NewWatcher() error = %v", err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	writeHotReloadConfig(t, fixture.configPath, hotReloadConfigBody("user-1"))
	configEvent := waitForWatchEvent(t, watcher.Events())
	if configEvent.Source != ReloadSourceConfig {
		t.Fatalf("config event source = %q, want config", configEvent.Source)
	}

	if err := os.RemoveAll(filepath.Dir(fixture.statePath)); err != nil {
		t.Fatalf("RemoveAll(state dir) error = %v", err)
	}
	stateEvent := waitForWatchEvent(t, watcher.Events())
	if stateEvent.Source != ReloadSourceState {
		t.Fatalf("state remove event source = %q, want state", stateEvent.Source)
	}
	waitForWatcherError(t, watcher.Errors())

	if err := SaveState(fixture.statePath, DefaultState()); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}
	stateEvent = waitForWatchEvent(t, watcher.Events())
	if stateEvent.Source != ReloadSourceState {
		t.Fatalf("state recreate event source = %q, want state", stateEvent.Source)
	}
}

func TestWatcherMergesConfigAndStateChangesWithinDebounceWindow(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	watcher, err := NewWatcher(fixture.configPath, fixture.statePath)
	if err != nil {
		t.Fatalf("NewWatcher() error = %v", err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	state := DefaultState()
	state.ApprovedUsers["user-1"] = ApprovedUser{
		ApprovedAt: time.Now().UTC(),
		Source:     "pairing",
	}

	writeHotReloadConfig(t, fixture.configPath, hotReloadConfigBody("user-1"))
	if err := SaveState(fixture.statePath, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	event := waitForWatchEvent(t, watcher.Events())
	if event.Source != ReloadSourceBoth {
		t.Fatalf("event source = %q, want both", event.Source)
	}
}

func TestWatcherHandleStateDirEventReportsUnavailableDirectory(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	watcher, err := NewWatcher(fixture.configPath, fixture.statePath)
	if err != nil {
		t.Fatalf("NewWatcher() error = %v", err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	err = watcher.handleStateDirEvent(fsnotify.Event{
		Name: watcher.stateDir,
		Op:   fsnotify.Remove,
	})
	if err == nil {
		t.Fatal("handleStateDirEvent() error = nil, want unavailable error")
	}
	if got, want := err.Error(), "watch access state dir: "+watcher.stateDir+" unavailable"; got != want {
		t.Fatalf("handleStateDirEvent() error = %q, want %q", got, want)
	}
}

func TestWatcherHandleStateDirEventIgnoresIrrelevantOps(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	watcher, err := NewWatcher(fixture.configPath, fixture.statePath)
	if err != nil {
		t.Fatalf("NewWatcher() error = %v", err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	if err := watcher.handleStateDirEvent(fsnotify.Event{
		Name: watcher.stateDir,
		Op:   fsnotify.Chmod,
	}); err != nil {
		t.Fatalf("handleStateDirEvent() error = %v, want nil", err)
	}
}

func TestWatcherCloseIsConcurrentSafe(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	watcher, err := NewWatcher(fixture.configPath, fixture.statePath)
	if err != nil {
		t.Fatalf("NewWatcher() error = %v", err)
	}

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = watcher.Close()
		}()
	}
	wg.Wait()
}

func TestWatcherHandlesMissingStateDirAtStartup(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	if err := os.RemoveAll(filepath.Dir(fixture.statePath)); err != nil {
		t.Fatalf("RemoveAll(state dir) error = %v", err)
	}

	watcher, err := NewWatcher(fixture.configPath, fixture.statePath)
	if err != nil {
		t.Fatalf("NewWatcher() error = %v", err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	if err := SaveState(fixture.statePath, DefaultState()); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	event := waitForWatchEvent(t, watcher.Events())
	if event.Source != ReloadSourceState {
		t.Fatalf("event source = %q, want state", event.Source)
	}
}

// --- Helpers ---

func waitForWatchEvent(t *testing.T, events <-chan WatchEvent) WatchEvent {
	t.Helper()

	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("watch event channel closed before event arrived")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("watch event did not arrive before timeout")
	}
	return WatchEvent{}
}

func waitForWatcherError(t *testing.T, errors <-chan error) error {
	t.Helper()

	select {
	case err, ok := <-errors:
		if !ok {
			t.Fatal("watch error channel closed before error arrived")
		}
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("watch error did not arrive before timeout")
	}
	return nil
}
