//go:build unix

package bizlogic

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRuleReplacementPreservesModeUnderUmask(t *testing.T) {
	// umask is process-wide; change it only in a dedicated subprocess.
	if os.Getenv("RELOADER_TEST_UMASK") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRuleReplacementPreservesModeUnderUmask$")
		cmd.Env = append(os.Environ(), "RELOADER_TEST_UMASK=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("umask subprocess: %v\n%s", err, output)
		}
		return
	}
	old := syscall.Umask(0077)
	defer syscall.Umask(old)
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, mode := range []os.FileMode{0644, 0640, 0600, 0777, 0440} {
		path := filepath.Join(dir, "rules.yml")
		if err := os.WriteFile(path, []byte("old"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := writeRegularConfig(root, "rules.yml", []byte("new"), 0); err != nil {
			t.Fatal(err)
		}
		info, err := root.Stat("rules.yml")
		if err != nil || info.Mode().Perm() != mode&0644 {
			t.Fatalf("existing mode %04o not preserved: %v, %v", mode, info, err)
		}
	}
	if err := writeRegularConfig(root, "new.yml", []byte("new"), os.O_CREATE); err != nil {
		t.Fatal(err)
	}
	info, err := root.Stat("new.yml")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("new file did not respect umask: %v, %v", info, err)
	}
}

func TestRuleReplacementPreservesOwnership(t *testing.T) {
	uid, gid := os.Geteuid(), -1
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if group != os.Getegid() {
			gid = group
			break
		}
	}
	if uid == 0 {
		uid, gid = 1, 1 // Root can also exercise preservation of another owner's UID.
	}
	if gid == -1 {
		t.Skip("no supplementary group available")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yml")
	if err := os.WriteFile(path, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := writeRegularConfig(root, "rules.yml", []byte("new"), 0); err != nil {
		t.Fatal(err)
	}
	info, err := root.Stat("rules.yml")
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if int(stat.Uid) != uid || int(stat.Gid) != gid || info.Mode().Perm() != 0640 {
		t.Fatalf("replacement metadata: uid=%d gid=%d mode=%04o; want %d:%d 0640", stat.Uid, stat.Gid, info.Mode().Perm(), uid, gid)
	}
}
