package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
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

func TestLoadValidatesBcryptEncoding(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(hash)
	cases := map[string]string{
		"invalid salt":          valid[:7] + strings.Repeat("!", 22) + valid[29:],
		"invalid checksum":      valid[:29] + strings.Repeat("!", 31),
		"truncated":             valid[:len(valid)-1],
		"trailing data":         valid + "x",
		"invalid separator":     valid[:6] + "!" + valid[7:],
		"unsupported version":   "$2z$" + valid[4:],
		"salt padding bits":     valid[:28] + "B" + valid[29:],
		"checksum padding bits": valid[:59] + "B",
		"salt newline":          valid[:7] + "\n" + valid[8:],
		"low cost":              valid[:4] + "09" + valid[6:],
		"high cost":             valid[:4] + "15" + valid[6:],
	}
	for name, hash := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadTestHash(t, hash); err == nil {
				t.Fatal("malformed bcrypt hash accepted")
			}
		})
	}
	for cost := 10; cost <= 14; cost++ {
		t.Run(fmt.Sprintf("valid cost %d", cost), func(t *testing.T) {
			hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), cost)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := loadTestHash(t, string(hash)); err != nil {
				t.Fatalf("valid bcrypt hash rejected: %v", err)
			}
		})
	}
	for _, version := range []string{"2b", "2y"} {
		t.Run(version, func(t *testing.T) {
			if _, err := loadTestHash(t, "$"+version+valid[3:]); err != nil {
				t.Fatalf("valid bcrypt variant rejected: %v", err)
			}
		})
	}
}

func loadTestHash(t *testing.T, hash string) (*Policy, error) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"users": map[string]User{"admin": {PasswordHash: hash, Role: "admin"}}})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	return Load(file)
}
