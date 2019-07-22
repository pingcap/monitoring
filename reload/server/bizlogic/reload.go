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
	defer watcher.Close()

	go func() {
		r.watch(watcher)
	}()

	return watcher.Add(r.wathDir)
}

func (r *Reloader) watch(watcher *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <- watcher.Events:
			if !ok {
				return
			}

			if !r.isValidEvent(event) {
				return
			}

			if err := r.promReload(); err != nil {
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

func (r *Reloader) promReload() error {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/-/reload", r.promUrl), nil)
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
	log.Println("successfully triggered reload")
	return nil
}

func (r *Reloader) isValidEvent(event fsnotify.Event) bool {
	if event.Op&fsnotify.Write == fsnotify.Write {
		return true
	}
	if event.Op&fsnotify.Create == fsnotify.Create {
		return false
	}

	return false
}
