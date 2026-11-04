//go:build unix

package bizlogic

import (
	"net/http"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestConfigRejectsFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(dir, "pipe.yml"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, dir, false, "")
	files, err := s.getConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("FIFO listed as config: %v", files)
	}
	if w := invokeConfig(t, s, http.MethodGet, "pipe.yml", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("FIFO read returned %d: %s", w.Code, w.Body)
	}
}
