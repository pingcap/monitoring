package common

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-github/v66/github"
)

func TestGetTreeKeepsLiteralBundlePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support colon filenames")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "monitor-snapshot", "test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"tidb.json": "original", "tidb.json:OWNERS": "literal colon contents"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Tree []*github.TreeEntry `json:"tree"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, e := range request.Tree {
			got[e.GetPath()] = e.GetContent()
		}
		if len(got) != 2 || got["monitor-snapshot/test/tidb.json"] != "original" || got["monitor-snapshot/test/tidb.json:OWNERS"] != "literal colon contents" {
			t.Errorf("unexpected commit tree: %#v", got)
		}
		_, _ = w.Write([]byte(`{"sha":"tree"}`))
	}))
	defer server.Close()
	client := github.NewClient(server.Client())
	client.BaseURL, _ = url.Parse(server.URL + "/")
	ref := &github.Reference{Object: &github.GitObject{SHA: github.String("base")}}
	if _, err := GetTree(client, ref, dir, context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("tree was not submitted: %d", calls)
	}
	// A requested source outside the output root must fail before any GitHub write.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private.json"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := GetTree(client, ref, outside, context.Background(), root); err == nil {
		t.Fatal("outside source accepted")
	}
	if calls != 1 {
		t.Fatal("outside source submitted to GitHub")
	}
}
