package apis

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnconfiguredServiceDoesNotExposeRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "private.yml"), []byte("groups: []"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := NewService(nil, dir, false, "")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/monitoring/configs", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured API is open: %d %s", w.Code, w.Body)
	}
}

func TestAuthenticationRolesAndAudit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "rules.yml")
	if err := os.WriteFile(file, []byte("groups: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	engine := NewSecuredService(u, dir, false, "", testPolicy(t))
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	for _, tc := range []struct {
		name, user, password, method, path string
		want                               int
	}{
		{"anonymous", "", "", http.MethodGet, "/monitoring/configs", 401},
		{"bad password", "admin", "wrong", http.MethodGet, "/monitoring/configs", 401},
		{"unknown user", "nobody", "test-password", http.MethodGet, "/monitoring/configs", 401},
		{"viewer read", "viewer", "test-password", http.MethodGet, "/monitoring/configs", 200},
		{"viewer write", "viewer", "test-password", http.MethodPut, "/monitoring/configs/rules.yml", 403},
		{"admin write", "admin", "test-password", http.MethodPut, "/monitoring/configs/rules.yml", 200},
		{"console protected", "", "", http.MethodGet, "/", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"content":"groups: []"}`))
			if tc.user != "" {
				r.SetBasicAuth(tc.user, tc.password)
			}
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("unexpected status: %d %s", w.Code, w.Body)
			}
			if tc.want == 401 && w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing login challenge")
			}
		})
	}
	for _, required := range []string{`"actor":"admin"`, `"actor":"viewer"`, `"config_sha256"`, `"config_saved":true`, `"event":"auth_rejected"`} {
		if !strings.Contains(logs.String(), required) {
			t.Fatal("audit field missing", required)
		}
	}
	for _, secret := range []string{"test-password", testPasswordHash, `groups: []`} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("audit log leaked credentials or rule content")
		}
	}
}
