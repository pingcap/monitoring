package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	PasswordHash string `json:"password_hash"`
	Role         string `json:"role"`
}

type Policy struct {
	users     map[string]User
	dummyHash string
	slots     chan struct{}
}

// Load reads bcrypt hashes and roles from a mounted file, never command-line passwords.
func Load(path string) (*Policy, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open auth file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("auth file must be regular")
	}
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return nil, fmt.Errorf("cannot read auth file within size limit")
	}
	var config struct {
		Users map[string]User `json:"users"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("invalid auth configuration")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("auth configuration must be a single JSON value")
	}
	if len(config.Users) == 0 {
		return nil, fmt.Errorf("auth configuration has no users")
	}
	p := &Policy{users: config.Users, slots: make(chan struct{}, 8)}
	for name, user := range config.Users {
		if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, ":\r\n") {
			return nil, fmt.Errorf("invalid auth username")
		}
		if user.Role != "admin" && user.Role != "viewer" {
			return nil, fmt.Errorf("invalid auth role")
		}
		cost, err := bcrypt.Cost([]byte(user.PasswordHash))
		if err != nil || cost < 10 || cost > 14 {
			return nil, fmt.Errorf("bcrypt password cost must be between 10 and 14")
		}
		p.dummyHash = user.PasswordHash
	}
	return p, nil
}

// Middleware also protects the static console so browsers can present a Basic-auth challenge.
func Middleware(p *Policy) gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "administrative API authentication is not configured"})
			return
		}
		select {
		case p.slots <- struct{}{}:
			defer func() { <-p.slots }()
		default:
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many authentication requests"})
			return
		}
		name, password, ok := c.Request.BasicAuth()
		user, exists := p.users[name]
		hash := p.dummyHash
		if exists {
			hash = user.PasswordHash
		}
		// Unknown usernames do the same bounded bcrypt work as known ones.
		valid := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
		if !ok || !exists || !valid {
			slog.Warn("reloader authentication rejected", "event", "auth_rejected", "remote", c.Request.RemoteAddr)
			c.Header("WWW-Authenticate", `Basic realm="monitoring", charset="UTF-8"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		c.Set("actor", name)
		c.Set("role", user.Role)
		if user.Role != "admin" && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			slog.Warn("reloader authorization rejected", "event", "authorization_rejected", "actor", name, "role", user.Role, "method", c.Request.Method)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "administrator role required"})
			return
		}
		c.Next()
	}
}

// HasAdmins reports whether this policy enables rule editing.
func (p *Policy) HasAdmins() bool {
	if p == nil {
		return false
	}
	for _, user := range p.users {
		if user.Role == "admin" {
			return true
		}
	}
	return false
}
