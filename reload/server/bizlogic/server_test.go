package bizlogic

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gin-gonic/gin"
)

func invokeConfig(t *testing.T, s *server, method, name, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "config", Value: name}}
	c.Request = httptest.NewRequest(method, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if method == http.MethodGet {
		s.GetConfig(c)
	} else {
		s.UpdateConfig(c)
	}
	return w
}

func TestConfigFileBoundary(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yml")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.yml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules.yml"), []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.txt"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, dir, false, "")
	for _, name := range []string{"../outside.yml", outside, "config.txt", "escape.yml", "missing.yml"} {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			t.Run(method+name, func(t *testing.T) {
				w := invokeConfig(t, s, method, name, `{"content":"groups: []"}`)
				if w.Code == http.StatusOK || strings.Contains(w.Body.String(), "private") {
					t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
				}
			})
		}
	}
	if w := invokeConfig(t, s, http.MethodGet, "rules.yml", ""); w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "private" {
		t.Fatalf("outside file changed: %q %v", data, err)
	}
}

func TestListConfigsFileBoundary(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yml")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules.yml"), []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.txt"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "directory.yml"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"alias.yml":             "rules.yml",
		"escape.yml":            outside,
		"outside-directory.yml": t.TempDir(),
		"dangling.yml":          "missing.yml",
		"directory-link.yml":    "directory.yml",
	} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	files, err := NewServer(nil, dir, false, "").getConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, []string{"alias.yml", "rules.yml"}) {
		t.Fatalf("unexpected config list: %v", files)
	}
}

func TestUpdateConfigValidationAndPersistence(t *testing.T) {
	dir, store := t.TempDir(), t.TempDir()
	file := filepath.Join(dir, "rules.yml")
	if err := os.WriteFile(file, []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, dir, true, store)
	for _, body := range []string{`{"content":"not: valid rules"}`, `{"content":"` + strings.Repeat("x", maxConfigBytes) + `"}`} {
		if w := invokeConfig(t, s, http.MethodPut, "rules.yml", body); w.Code == http.StatusOK {
			t.Fatal("invalid input accepted")
		}
		data, _ := os.ReadFile(file)
		if string(data) != "groups: []" {
			t.Fatal("invalid input was written")
		}
		if _, err := os.Stat(filepath.Join(store, "rules.yml")); !os.IsNotExist(err) {
			t.Fatal("invalid input persisted")
		}
	}
	content := "groups:\n- name: test\n  rules:\n  - alert: TestAlert\n    expr: up == 0\n"
	body, _ := json.Marshal(map[string]string{"content": content})
	if w := invokeConfig(t, s, http.MethodPut, "rules.yml", string(body)); w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body)
	}
	for _, path := range []string{file, filepath.Join(store, "rules.yml")} {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, []byte(content)) {
			t.Fatalf("bad saved file: %s %v", path, err)
		}
	}
	info, _ := os.Stat(filepath.Join(store, "rules.yml"))
	if info.Mode().Perm()&0022 != 0 {
		t.Fatal("persistence file is writable by other users")
	}
	// A missing primary file must not be created or mirrored.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if w := invokeConfig(t, s, http.MethodPut, "rules.yml", string(body)); w.Code != http.StatusNotFound {
		t.Fatal(w.Code, w.Body)
	}
}

func TestWatcherContinuesAfterUnknownEvent(t *testing.T) {
	requests := make(chan struct{}, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests <- struct{}{}; w.WriteHeader(http.StatusOK) }))
	defer endpoint.Close()
	u, _ := url.Parse(endpoint.URL)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	// Inject events on a separate channel; the OS backend owns and closes its channel.
	watcher.Events = make(chan fsnotify.Event)
	done := make(chan struct{})
	go func() { defer close(done); NewReloader(u, t.TempDir()).watch(watcher) }()
	watcher.Events <- fsnotify.Event{Name: "rules.yml"}
	select {
	case watcher.Events <- fsnotify.Event{Name: "rules.yml", Op: fsnotify.Write}:
	case <-done:
		t.Fatal("watcher stopped on unknown event")
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not receive next event")
	}
	select {
	case <-requests:
	case <-time.After(3 * time.Second):
		t.Fatal("no reload after valid event")
	}
	watcher.Close()
	<-done
}
