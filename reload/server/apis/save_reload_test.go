package apis

import (
	"encoding/json"
	"github.com/pingcap/monitoring/reload/server/bizlogic"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSaveWaitsForReloadBeforeRefreshingRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.yml"), []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var live atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/-/reload":
			close(started)
			select {
			case <-release:
				live.Store(true)
				w.WriteHeader(http.StatusOK)
			case <-r.Context().Done():
			}
		case "/api/v1/rules":
			if live.Load() {
				_, _ = w.Write([]byte(`{"version":"new"}`))
			} else {
				_, _ = w.Write([]byte(`{"version":"old"}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	// Always release a blocked upstream before closing the test server, including on failures.
	defer close(release)
	u, _ := url.Parse(upstream.URL)
	engine := NewService(u, dir, false, "", bizlogic.NewReloader(u, dir))
	body, _ := json.Marshal(map[string]string{"content": "groups: []"})
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/monitoring/configs/rules.yml", strings.NewReader(string(body))))
		result <- w
	}()
	select {
	case <-started:
	case w := <-result:
		t.Fatalf("save returned before reload: %d %s", w.Code, w.Body)
	case <-time.After(2 * time.Second):
		t.Fatal("save never requested reload")
	}
	select {
	case w := <-result:
		t.Fatalf("save returned while reload was pending: %d", w.Code)
	default:
	}
	release <- struct{}{}
	select {
	case w := <-result:
		if w.Code != http.StatusOK {
			t.Fatal(w.Code, w.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("save did not finish after reload")
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/monitoring/rules", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"new"`) {
		t.Fatalf("first UI refresh is stale: %d %s", w.Code, w.Body)
	}
}

func TestSaveReportsReloadFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.yml"), []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	engine := NewService(u, dir, false, "", bizlogic.NewReloader(u, dir))
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/monitoring/configs/rules.yml", strings.NewReader(`{"content":"groups: []"}`)))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "saved") {
		t.Fatalf("reload failure reported as success: %d %s", w.Code, w.Body)
	}
}
