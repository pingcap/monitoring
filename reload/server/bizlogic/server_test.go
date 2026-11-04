package bizlogic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
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
	escapeErr := os.Symlink(outside, filepath.Join(dir, "escape.yml"))
	if escapeErr != nil && !symlinkPrivilegeUnavailable(escapeErr) {
		t.Fatal(escapeErr)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules.yml"), []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.txt"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	s := newConfigTestServer(nil, dir, false, "")
	for _, name := range []string{"../outside.yml", outside, "config.txt", "escape.yml", "missing.yml"} {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			t.Run(method+name, func(t *testing.T) {
				if name == "escape.yml" && escapeErr != nil {
					t.Skipf("symbolic link privilege unavailable: %v", escapeErr)
				}
				w := invokeConfig(t, s, method, name, `{"content":"groups: []"}`)
				if w.Code == http.StatusOK || strings.Contains(w.Body.String(), "private") {
					t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
				}
			})
		}
	}
	w := invokeConfig(t, s, http.MethodGet, "rules.yml", "")
	var got string
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil || got != "groups: []" {
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
	linked := map[string]bool{}
	for name, target := range map[string]string{
		"alias.yml":             "rules.yml",
		"escape.yml":            outside,
		"outside-directory.yml": t.TempDir(),
		"dangling.yml":          "missing.yml",
		"directory-link.yml":    "directory.yml",
	} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			if !symlinkPrivilegeUnavailable(err) {
				t.Fatal(err)
			}
			t.Logf("symbolic link %s unavailable: %v", name, err)
			continue
		}
		linked[name] = true
	}
	files, err := newConfigTestServer(nil, dir, false, "").getConfigs()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"rules.yml"}
	if linked["alias.yml"] {
		want = []string{"alias.yml", "rules.yml"}
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("unexpected config list: %v", files)
	}
}

func TestUpdateConfigValidationAndPersistence(t *testing.T) {
	dir, store := t.TempDir(), t.TempDir()
	file := filepath.Join(dir, "rules.yml")
	if err := os.WriteFile(file, []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newConfigTestServer(nil, dir, true, store)
	for _, body := range []string{`{"content":"not: valid rules"}`, `{"content":"` + strings.Repeat("x", maxConfigBytes) + `"}`} {
		if w := invokeConfig(t, s, http.MethodPut, "rules.yml", body); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid input not rejected with 400: %d", w.Code)
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

func TestUpdateRejectsTrailingBody(t *testing.T) {
	for name, suffix := range map[string]string{
		"second JSON": ` {"extra":true}`, "garbage": "garbage", "oversized whitespace": strings.Repeat(" ", maxConfigBytes),
	} {
		t.Run(name, func(t *testing.T) {
			dir, store := t.TempDir(), t.TempDir()
			file := filepath.Join(dir, "rules.yml")
			if err := os.WriteFile(file, []byte("groups: []"), 0644); err != nil {
				t.Fatal(err)
			}
			w := invokeConfig(t, newConfigTestServer(nil, dir, true, store), http.MethodPut, "rules.yml", `{"content":"groups: []"}`+suffix)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("trailing body accepted: %d", w.Code)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != "groups: []" {
				t.Fatalf("invalid body changed primary file: %q %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(store, "rules.yml")); !os.IsNotExist(err) {
				t.Fatal("invalid body persisted")
			}
		})
	}
	// Whitespace within the limit remains legal JSON.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.yml"), []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	if w := invokeConfig(t, newConfigTestServer(nil, dir, false, ""), http.MethodPut, "rules.yml", `{"content":"groups: []"}`+" \n\t"); w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body)
	}
}

type beforeRead struct {
	io.Reader
	before func()
}

func (r *beforeRead) Read(p []byte) (int, error) {
	if r.before != nil {
		f := r.before
		r.before = nil
		f()
	}
	return r.Reader.Read(p)
}

func TestUpdateDoesNotRecreateRemovedFile(t *testing.T) {
	dir, store := t.TempDir(), t.TempDir()
	file := filepath.Join(dir, "rules.yml")
	if err := os.WriteFile(file, []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "config", Value: "rules.yml"}}
	reader := &beforeRead{Reader: strings.NewReader(`{"content":"groups: []"}`), before: func() {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}}
	c.Request = httptest.NewRequest(http.MethodPut, "/", reader)
	c.Request.Header.Set("Content-Type", "application/json")
	newConfigTestServer(nil, dir, true, store).UpdateConfig(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("removed file accepted: %d %s", w.Code, w.Body)
	}
	for _, path := range []string{file, filepath.Join(store, "rules.yml")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("file recreated: %s", path)
		}
	}
}

func TestListConfigsSizeBoundary(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]int64{"limit.yml": maxConfigBytes, "oversized.yml": maxConfigBytes + 1} {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	files, err := newConfigTestServer(nil, dir, false, "").getConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, []string{"limit.yml"}) {
		t.Fatal("unexpected config list:", files)
	}
}

// ERROR_PRIVILEGE_NOT_HELD is returned by Windows when symlink creation is not permitted.
func symlinkPrivilegeUnavailable(err error) bool {
	return runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314))
}

// File-operation tests isolate persistence from the separately tested Prometheus acknowledgement.
func newConfigTestServer(url *url.URL, dir string, persist bool, store string) *server {
	s := NewServer(url, dir, persist, store)
	s.reload = func(context.Context) error { return nil }
	return s
}
