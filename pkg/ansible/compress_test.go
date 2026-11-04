package ansible

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompressUsesRelativeMemberNames(t *testing.T) {
	source := filepath.Join(t.TempDir(), "ansible-monitor")
	if err := os.MkdirAll(filepath.Join(source, "rules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "rules", "test.yml"), []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := Compress(source, dest); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if path.IsAbs(h.Name) || path.Clean(h.Name) != h.Name || strings.HasPrefix(h.Name, "../") {
			t.Fatalf("unsafe member name %q", h.Name)
		}
		if h.Name == "ansible-monitor/rules/test.yml" {
			data, err := io.ReadAll(tr)
			if err != nil || string(data) != "groups: []" {
				t.Fatalf("bad archived data: %s, %v", data, err)
			}
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected rule file once, got %d", count)
	}
}
