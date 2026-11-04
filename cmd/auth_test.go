package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/pingcap/monitoring/pkg/common"
)

type captureAuthTransport func(*http.Request) (*http.Response, error)

func (f captureAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRepoServiceAuthentication(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, tt := range []struct {
		name   string
		config Config
		want   string
	}{
		{name: "anonymous"},
		{name: "basic", config: Config{UserName: "user", Password: "password"}, want: "Basic dXNlcjpwYXNzd29yZA=="},
		{name: "password only", config: Config{Password: "password"}, want: "Basic OnBhc3N3b3Jk"},
		{name: "username only", config: Config{UserName: "user"}, want: "Basic dXNlcjo="},
		{name: "token takes precedence", config: Config{Token: "test-token", UserName: "user", Password: "password"}, want: "Bearer test-token"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			http.DefaultTransport = captureAuthTransport(func(r *http.Request) (*http.Response, error) {
				called = true
				if got := r.Header.Get("Authorization"); got != tt.want {
					t.Errorf("Authorization = %q, want %q", got, tt.want)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("[]"))}, nil
			})
			service, err := RepoService(&tt.config)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := service.GetContents("pingcap", "monitoring", "rules", &common.RepositoryContentGetOptions{}); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("no request made")
			}
		})
	}
}
