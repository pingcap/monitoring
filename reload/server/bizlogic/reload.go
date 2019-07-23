package bizlogic

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
	"log"
	"net/http"
	"net/url"
)

type Reloader struct {
	promUrl *url.URL
	wathDir string
}

func NewReloader(url *url.URL, watchDir string) *Reloader {
	return &Reloader{
		promUrl: url,
		wathDir: watchDir,
	}
}

func (r *Reloader) Start() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	go func() {
		r.watch(watcher)
	}()

	return watcher.Add(r.wathDir)
}

func (r *Reloader) watch(watcher *fsnotify.Watcher) {
	defer watcher.Close()
	for {
		select {
		case event, ok := <- watcher.Events:
			if !ok {
				return
			}

			if !r.isValidEvent(event) {
				return
			}

			if err := r.promReload(event); err != nil {
				log.Println("reload failed", err)
			}
		case err, ok := <- watcher.Errors:
			if !ok {
				return
			}

			log.Println("error:", err)
		}
	}
}

func (r *Reloader) promReload(event fsnotify.Event) error {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/-/reload", r.promUrl), nil)
	if err != nil {
		return errors.Wrap(err,"init prometheus request failed")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "request prometheus failed")
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("Received error response code, %d", resp.StatusCode))
	}
	log.Println(fmt.Printf("successfully triggered reload, event=%s, name=%s", event.Op.String(), event.Name))
	return nil
}

func (r *Reloader) isValidEvent(event fsnotify.Event) bool {
	if event.Op&fsnotify.Write == fsnotify.Write {
		return true
	}
	if event.Op&fsnotify.Create == fsnotify.Create {
		return true
	}

	if event.Op&fsnotify.Chmod == fsnotify.Chmod {
		return true
	}

	if event.Op&fsnotify.Remove == fsnotify.Remove {
		return true
	}

	if event.Op&fsnotify.Rename == fsnotify.Rename {
		return true
	}

	return false
}
