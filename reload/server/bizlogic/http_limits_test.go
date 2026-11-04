package bizlogic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gin-gonic/gin"
)

func TestListRulesRejectsOversizedResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", (16<<20)+1))) }))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	s := newConfigTestServer(u, t.TempDir(), false, "")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	s.ListRules(c)
	if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), strings.Repeat("x", 100)) {
		t.Fatalf("oversized response accepted: %d", w.Code)
	}
}

func TestListRulesHonorsRequestCancellation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	s := newConfigTestServer(u, t.TempDir(), false, "")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { defer close(done); s.ListRules(c) }()
	select {
	case <-done:
		if w.Code != http.StatusBadGateway {
			t.Fatal(w.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled rule query is still running")
	}
}

func TestPrometheusClientsHaveDeadlines(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	s := newConfigTestServer(u, t.TempDir(), false, "")
	reloader := NewReloader(u, t.TempDir())
	if s.httpClient.Timeout <= 0 || reloader.httpClient.Timeout <= 0 {
		t.Fatal("Prometheus clients have no deadline")
	}
	s.httpClient.Timeout = 20 * time.Millisecond
	reloader.httpClient.Timeout = 20 * time.Millisecond
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	s.ListRules(c)
	if w.Code != http.StatusBadGateway {
		t.Fatal("slow upstream accepted", w.Code)
	}
	if err := reloader.promReload(fsnotify.Event{}); err == nil {
		t.Fatal("slow reload was not terminated")
	}
}

func TestWatcherCoalescesWriteBursts(t *testing.T) {
	requests := make(chan struct{}, 20)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests <- struct{}{}; w.WriteHeader(http.StatusOK) }))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	watcher.Events = make(chan fsnotify.Event)
	done := make(chan struct{})
	go func() { defer close(done); NewReloader(u, t.TempDir()).watch(watcher) }()
	defer func() { watcher.Close(); <-done }()
	for i := 0; i < 10; i++ {
		watcher.Events <- fsnotify.Event{Name: "rules.yml", Op: fsnotify.Write}
	}
	select {
	case <-requests:
	case <-time.After(3 * time.Second):
		t.Fatal("write burst never reloaded")
	}
	select {
	case <-requests:
		t.Fatal("write burst triggered repeated immediate reloads")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSharedReloadWaitIsCancelableAndRateLimited(t *testing.T) {
	calls := make(chan time.Time, 2)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls <- time.Now()
		select {
		case <-release:
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer close(release)
	u, _ := url.Parse(upstream.URL)
	reloader := NewReloader(u, t.TempDir())
	first := make(chan error, 1)
	go func() { first <- reloader.Reload(context.Background()) }()
	var started time.Time
	select {
	case started = <-calls:
	case <-time.After(time.Second):
		t.Fatal("first reload did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	queued := make(chan error, 1)
	go func() { queued <- reloader.Reload(ctx) }()
	select {
	case err := <-queued:
		if err == nil {
			t.Fatal("canceled queued reload succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled reload did not leave shared queue")
	}
	release <- struct{}{}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	next := make(chan error, 1)
	go func() { next <- reloader.Reload(context.Background()) }()
	select {
	case at := <-calls:
		if at.Sub(started) < time.Second-5*time.Millisecond {
			t.Fatal("shared reload rate limit bypassed", at.Sub(started))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("next reload never started")
	}
	release <- struct{}{}
	if err := <-next; err != nil {
		t.Fatal(err)
	}
}
