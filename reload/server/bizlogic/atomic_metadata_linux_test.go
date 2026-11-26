//go:build linux

package bizlogic

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRuleReplacementRejectsOwnershipFailure(t *testing.T) {
	if dir := os.Getenv("RELOADER_TEST_OWNERSHIP_DIR"); dir != "" {
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		err = writeRegularConfig(root, "rules.yml", []byte("new"), 0)
		if err == nil || !strings.Contains(err.Error(), "preserve config ownership") {
			t.Fatalf("expected ownership failure, got %v", err)
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root to create a file owned by another user")
	}
	// A writable directory allows rename, but the child cannot chown to root.
	dir, err := os.MkdirTemp("", "reloader-ownership-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rules.yml")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	// go test may put its binary below a root-only build directory.
	binary := filepath.Join(dir, "metadata.test")
	executable, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, executable, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(binary, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestRuleReplacementRejectsOwnershipFailure$")
	cmd.Env = append(os.Environ(), "RELOADER_TEST_OWNERSHIP_DIR="+dir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unprivileged subprocess: %v\n%s", err, output)
	}
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatalf("failed save replaced original: %q, %v", data, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("failed save leaked temporary files: %v, %v", files, err)
	}
}
