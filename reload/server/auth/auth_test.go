package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidAuthFilesFailClosed(t *testing.T) {
	for name, body := range map[string]string{"empty users": `{"users":{}}`, "plaintext": `{"users":{"admin":{"password_hash":"password","role":"admin"}}}`, "unknown field": `{"users":{},"password":"oops"}`, "trailing JSON": `{"users":{}} {}`, "oversized": strings.Repeat("x", (64<<10)+1)} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "auth.json")
			if err := os.WriteFile(file, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(file); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
