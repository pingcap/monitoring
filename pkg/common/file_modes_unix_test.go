//go:build unix

package common

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestGeneratedFilePermissions(t *testing.T) {
	mask := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(mask) })
	dir := t.TempDir()
	for name, want := range map[string]os.FileMode{"rules.yml": 0644, "Dockerfile": 0644, "init.sh": 0755} {
		WriteFile(dir, name, "test")
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", name, info.Mode().Perm(), want)
		}
	}
}
