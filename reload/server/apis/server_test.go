package apis

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestMonitoringConcurrencyBudget(t *testing.T) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	engine := NewService(u, t.TempDir(), false, "")
	var wg sync.WaitGroup
	defer wg.Wait()
	defer close(release)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/monitoring/rules", nil))
			if w.Code != http.StatusOK {
				t.Errorf("accepted request failed: %d", w.Code)
			}
		}()
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("upstream request not started")
		}
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/monitoring/rules", nil))
		result <- w
	}()
	select {
	case w := <-result:
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("excess concurrency accepted: %d", w.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("excess request was not rejected immediately")
	}

}
