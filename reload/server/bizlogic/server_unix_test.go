//go:build unix

package bizlogic

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestConfigRejectsFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(dir, "pipe.yml"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, dir, false, "")
	files, err := s.getConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("FIFO listed as config: %v", files)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- invokeConfig(t, s, http.MethodGet, "pipe.yml", "") }()
	select {
	case w := <-done:
		if w.Code != http.StatusBadRequest {
			t.Fatalf("FIFO read returned %d: %s", w.Code, w.Body)
		}
	case <-time.After(time.Second):
		// Unblock a regressed blocking open before reporting failure.
		fd, err := unix.Open(filepath.Join(dir, "pipe.yml"), unix.O_RDWR|unix.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal("cannot unblock FIFO read:", err)
		}
		defer unix.Close(fd)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("FIFO read did not exit after unblocking")
		}
		t.Fatal("FIFO read blocked")
	}
}

func TestUpdateRejectsFIFOReplacement(t *testing.T) {
	dir, store := t.TempDir(), t.TempDir()
	path := filepath.Join(dir, "rules.yml")
	if err := os.WriteFile(path, []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "config", Value: "rules.yml"}}
	reader := &beforeRead{Reader: strings.NewReader(`{"content":"groups: []"}`), before: func() {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
	}}
	c.Request = httptest.NewRequest(http.MethodPut, "/", reader)
	c.Request.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() { defer close(done); NewServer(nil, dir, true, store).UpdateConfig(c) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal("cannot unblock FIFO write:", err)
		}
		defer unix.Close(fd)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("FIFO update did not exit after unblocking")
		}
		t.Fatal("FIFO update blocked")
	}
	if w.Code == http.StatusOK {
		t.Fatal("FIFO replacement accepted")
	}
	if _, err := os.Stat(filepath.Join(store, "rules.yml")); !os.IsNotExist(err) {
		t.Fatal("FIFO content persisted")
	}
}
