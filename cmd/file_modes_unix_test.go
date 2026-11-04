//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopiedFilePermissions(t *testing.T) {
	mask := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(mask) })
	base, dest := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "cmd"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{"Dockerfile": 0644, "init.sh": 0755} {
		if err := os.WriteFile(filepath.Join(base, "cmd", name), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
		copyLocalfiles(base, dest, name, "output")
		info, err := os.Stat(filepath.Join(dest, "output", name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", name, info.Mode().Perm(), want)
		}
	}
	info, err := os.Stat(filepath.Join(dest, "output"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("directory mode = %o", info.Mode().Perm())
	}
	writeFile(dest, "rules.yml", "groups: []")
	info, err = os.Stat(filepath.Join(dest, "rules.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("legacy rule file mode = %o", info.Mode().Perm())
	}
}
