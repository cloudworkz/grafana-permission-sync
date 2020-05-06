package watcher

import (
	"time"

	"github.com/bep/debounce"
	fsnotify "github.com/fsnotify/fsnotify"
)

// Watcher is a wrapper around fsnotify to provide a slightly better API. The intention is to eventually extend it.
type Watcher struct {
	watcher *fsnotify.Watcher

	filePath    string // path being watched
	keepRunning bool   // can be set to false (by "Stop()") to stop watching

	changes chan struct{}
	errors  chan error

	OnChange func(filePath string) // callback a user can set to listen for changes
	OnError  func(err error)       // callback a user can set to listen for errors
}

// WatchPath creates a "file watcher" that will notify you when the given file has been updated
func WatchPath(pathToFile string) (*Watcher, error) {

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{fsWatcher, pathToFile, true, make(chan struct{}), make(chan error), nil, nil}

	// doNotify := createDebounced(1000*time.Millisecond, func() {
	// 	w.changes <- struct{}{}
	// })

	debouncer := debounce.New(500 * time.Millisecond)
	doNotify := func(filePath string) {
		debouncer(func() {
			if !w.keepRunning {
				return
			}
			// w.changes <- struct{}{} // will block, we'd have to ensure it gets drained
			callback := w.OnChange
			if callback != nil {
				callback(filePath)
			}
		})
	}

	go func() {
		for w.keepRunning {
			select {
			case event := <-fsWatcher.Events:
				if !isValidEvent(event) {
					continue
				}
				doNotify(event.Name)

			case err := <-fsWatcher.Errors:
				//w.errors <- err
				h := w.OnError
				if h != nil {
					h(err)
				}
			}
		}
	}()

	err = fsWatcher.Add(pathToFile)
	if err != nil {
		w.Stop()
		return nil, err
	}

	return w, nil
}

// Stop stops the watcher, releases all resources, ...
func (w *Watcher) Stop() {
	if w.keepRunning == false {
		return
	}

	w.keepRunning = false
	w.watcher.Close()
}

func createDebounced(duration time.Duration, f func()) func() {
	debouncer := debounce.New(duration)
	debouncedFunc := func() { debouncer(f) }
	return debouncedFunc
}

func isValidEvent(event fsnotify.Event) bool {
	if (event.Op&fsnotify.Create == fsnotify.Create) ||
		(event.Op&fsnotify.Rename == fsnotify.Rename) ||
		(event.Op&fsnotify.Write == fsnotify.Write) {
		return true
	}

	return false
}
