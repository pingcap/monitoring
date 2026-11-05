package apis

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pingcap/monitoring/reload/server/auth"
	"github.com/pingcap/monitoring/reload/server/bizlogic"
	"golang.org/x/crypto/bcrypt"
)

var testHashOnce sync.Once
var testPasswordHash string

func testPolicy(t *testing.T) *auth.Policy {
	t.Helper()
	testHashOnce.Do(func() {
		hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), 10)
		if err != nil {
			panic(err)
		}
		testPasswordHash = string(hash)
	})
	body, err := json.Marshal(map[string]any{"users": map[string]auth.User{"admin": {PasswordHash: testPasswordHash, Role: "admin"}, "viewer": {PasswordHash: testPasswordHash, Role: "viewer"}}})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	policy, err := auth.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func newAuthenticatedTestService(t *testing.T, u *url.URL, dir string, persist bool, store string, reloaders ...*bizlogic.Reloader) http.Handler {
	t.Helper()
	engine := NewSecuredService(u, dir, persist, store, testPolicy(t), reloaders...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetBasicAuth("admin", "test-password")
		engine.ServeHTTP(w, r)
	})
}
