package common

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadContents(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, auth := range []string{"basic", "token"} {
		t.Run(auth, func(t *testing.T) {
			calls := 0
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Authorization") != "" || r.Header.Get(headerOTP) != "" {
					t.Fatal("API credential leaked to raw content request")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("rules"))}, nil
			})
			var service *GitRepoService
			var err error
			if auth == "basic" {
				service, err = NewGitRepoServiceWithAuth(BasicAuthTransport{Username: "user", Password: "push-secret", OTP: "otp"})
			} else {
				service, err = NewGitRepoServiceWithToken("push-secret")
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"http://raw.githubusercontent.com/file", "https://evil.example/file", "https://raw.githubusercontent.com.evil.example/file", "https://raw.githubusercontent.com:443/file", "https://user@raw.githubusercontent.com/file"} {
				if _, err := service.DownloadContents(&RepositoryContent{DownloadURL: &target}); err == nil {
					t.Fatalf("accepted %s", target)
				}
			}
			if calls != 0 {
				t.Fatal("invalid URL reached transport")
			}
			target := "https://raw.githubusercontent.com/pingcap/tidb/master/rules.yml"
			data, err := service.DownloadContents(&RepositoryContent{DownloadURL: &target})
			if err != nil || string(data) != "rules" || calls != 1 {
				t.Fatalf("download: %q, %v, calls=%d", data, err, calls)
			}
		})
	}
}

func TestDownloadContentsRejectsRedirect(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://evil.example/steal"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	service, err := NewGitRepoServiceWithToken("push-secret")
	if err != nil {
		t.Fatal(err)
	}
	target := "https://raw.githubusercontent.com/pingcap/tidb/master/rules.yml"
	name := "rules.yml"
	if _, err := service.DownloadContents(&RepositoryContent{DownloadURL: &target, Name: &name}); err == nil {
		t.Fatal("accepted redirect")
	}
	if calls != 1 {
		t.Fatalf("followed redirect: %d requests", calls)
	}
}

func TestDownloadContentsPrivateViaAPI(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	redirect := false
	calls := 0
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch r.URL.Host {
		case "raw.githubusercontent.com":
			if r.Header.Get("Authorization") != "" {
				t.Fatal("raw download received API token")
			}
			return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		case "api.github.com":
			if r.Header.Get("Authorization") != "Bearer push-secret" || r.Header.Get("Accept") != "application/vnd.github.raw+json" {
				t.Fatal("private API request lost authentication or raw media type")
			}
			if redirect {
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://evil.example/steal"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("private rules"))}, nil
		default:
			t.Fatalf("untrusted request host: %s", r.URL.Host)
			return nil, nil
		}
	})
	service, err := NewGitRepoServiceWithToken("push-secret")
	if err != nil {
		t.Fatal(err)
	}
	rawURL := "https://raw.githubusercontent.com/pingcap/private/master/rules.yml"
	apiURL := "https://api.github.com/repos/pingcap/private/contents/rules.yml?ref=master"
	name := "rules.yml"
	content := &RepositoryContent{DownloadURL: &rawURL, URL: &apiURL, Name: &name}
	data, err := service.DownloadContents(content)
	if err != nil || string(data) != "private rules" {
		t.Fatalf("private download: %q, %v", data, err)
	}
	redirect = true
	calls = 0
	if _, err := service.DownloadContents(content); err == nil {
		t.Fatal("accepted authenticated API redirect")
	}
	if calls != 2 {
		t.Fatalf("followed authenticated redirect: %d requests", calls)
	}
	apiURL = "https://evil.example/repos/pingcap/private/contents/rules.yml"
	calls = 0
	if _, err := service.DownloadContents(content); err == nil {
		t.Fatal("accepted untrusted API host")
	}
	if calls != 1 {
		t.Fatalf("untrusted API URL reached transport: %d requests", calls)
	}
}
