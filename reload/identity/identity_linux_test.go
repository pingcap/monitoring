//go:build linux

package identity

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPreparePaths(t *testing.T) {
	for _, attack := range []string{"none", "symlink-file", "hardlink-file", "symlink-dir", "symlink-parent", "fifo", "version", "dotdot"} {
		t.Run(attack, func(t *testing.T) {
			root := t.TempDir()
			active, data := filepath.Join(root, "active"), filepath.Join(root, "data")
			for _, p := range []string{active, data} {
				if err := os.Mkdir(p, 0755); err != nil {
					t.Fatal(err)
				}
			}
			outside := filepath.Join(root, "outside.yml")
			if err := os.WriteFile(outside, []byte("private"), 0400); err != nil {
				t.Fatal(err)
			}
			rule := filepath.Join(active, "custom.rules.yml")
			version := "tidb:v1"
			switch attack {
			case "symlink-file":
				os.Symlink(outside, rule)
			case "hardlink-file":
				os.Link(outside, rule)
			case "symlink-dir":
				os.Remove(active)
				os.Symlink(data, active)
			case "symlink-parent":
				os.Symlink(root, filepath.Join(data, "latest-rules"))
			case "fifo":
				if err := syscall.Mkfifo(rule, 0600); err != nil {
					t.Fatal(err)
				}
			case "dotdot":
				active = filepath.Join(root, "active") + "/../active"
			case "version":
				version = "../other"
			default:
				if err := os.WriteFile(rule, []byte("groups: []"), 0444); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(rule, 0444); err != nil {
					t.Fatal(err)
				}
			}
			err := preparePaths(active, data, version, os.Geteuid())
			if attack != "none" {
				if err == nil {
					t.Fatal("unsafe path accepted")
				}
				info, e := os.Stat(outside)
				if e != nil || info.Mode().Perm() != 0400 {
					t.Fatal("outside target modified", e)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(rule)
			if err != nil || info.Mode().Perm() != 0644 {
				t.Fatal("rule access not prepared", err)
			}
			if _, err := os.Stat(filepath.Join(data, "latest-rules", version)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDropAndExec(t *testing.T) {
	// This helper runs only in a disposable Linux test container as root.
	if os.Getenv("RELOADER_IDENTITY_TEST_CHILD") == "1" {
		if os.Geteuid() == 0 {
			groups, err := os.Getgroups()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Setenv("RELOADER_IDENTITY_EXPECTED_GROUPS", fmt.Sprint(groups)); err != nil {
				t.Fatal(err)
			}
			if err := dropAndExec(1000, 2000); err != nil {
				t.Fatal(err)
			}
			t.Fatal("exec returned")
		}
		if os.Getuid() != 1000 || os.Geteuid() != 1000 || os.Getgid() != 2000 || os.Getegid() != 2000 {
			t.Fatal("wrong runtime identity")
		}
		if err := Prepare("/does-not-exist", "/does-not-exist", "invalid"); err == nil {
			t.Fatal("non-root runtime did not check inaccessible paths")
		}
		groups, err := os.Getgroups()
		if err != nil || fmt.Sprint(groups) != os.Getenv("RELOADER_IDENTITY_EXPECTED_GROUPS") {
			t.Fatal("Kubernetes volume groups changed", groups, err)
		}
		status, err := os.ReadFile("/proc/self/status")
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			switch fields[0] {
			case "CapInh:", "CapPrm:", "CapEff:", "CapAmb:":
				if fields[1] != "0000000000000000" {
					t.Fatal("capabilities retained", line)
				}
			case "NoNewPrivs:":
				if fields[1] != "1" {
					t.Fatal("no_new_privs unset")
				}
			case "Uid:":
				if strings.Join(fields[1:], " ") != "1000 1000 1000 1000" {
					t.Fatal("saved uid retained", line)
				}
			case "Gid:":
				if strings.Join(fields[1:], " ") != "2000 2000 2000 2000" {
					t.Fatal("saved gid retained", line)
				}
			}
		}
		if syscall.Setuid(0) == nil {
			t.Fatal("regained root")
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires disposable root container")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDropAndExec$")
	cmd.Env = append(os.Environ(), "RELOADER_IDENTITY_TEST_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

func TestPreserveVolumeGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires disposable root container")
	}
	root := t.TempDir()
	active, data := filepath.Join(root, "active"), filepath.Join(root, "data")
	for _, p := range []string{active, data} {
		if err := os.Mkdir(p, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(p, 0, 1234); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0750|os.ModeSticky); err != nil {
			t.Fatal(err)
		}
	}
	rule := filepath.Join(active, "custom.rules.yml")
	if err := os.WriteFile(rule, []byte("groups: []"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(rule, 0, 1234); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(rule, 0640); err != nil {
		t.Fatal(err)
	}
	if err := preparePaths(active, data, "v1", 1000); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(rule)
	if err != nil {
		t.Fatal(err)
	}
	if info.Sys().(*syscall.Stat_t).Gid != 1234 || info.Mode().Perm() != 0640 {
		t.Fatal("reader's group access changed", info.Sys(), info.Mode())
	}
	info, err = os.Stat(active)
	if err != nil {
		t.Fatal(err)
	}
	if info.Sys().(*syscall.Stat_t).Gid != 1234 || info.Mode()&os.ModeSetgid == 0 || info.Mode()&os.ModeSticky == 0 {
		t.Fatal("atomic replacements will not inherit existing group")
	}
}

func TestRejectUnpreservableRuleGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires disposable root container")
	}
	dir := t.TempDir()
	name := filepath.Join(dir, "custom.rules.yml")
	if err := os.WriteFile(name, []byte("groups: []"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(name, os.Geteuid(), 60001); err != nil {
		t.Fatal(err)
	}
	opened, err := openDirectory(dir, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if err := checkReplacementGroups(opened); err == nil {
		t.Fatal("unsupported group would fail only after saving active rules")
	}
}

func TestRejectUnpreservableRuleOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires disposable root container")
	}
	dir := t.TempDir()
	name := filepath.Join(dir, "custom.rules.yml")
	if err := os.WriteFile(name, []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(name, 1000, os.Getegid()); err != nil {
		t.Fatal(err)
	}
	opened, err := openDirectory(dir, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if err := checkReplacementGroups(opened); err == nil {
		t.Fatal("unsupported owner would fail only after saving active rules")
	}
}

func TestWatchOnlyDoesNotRequireRuleOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires disposable root container")
	}
	dir := t.TempDir()
	name := filepath.Join(dir, "custom.rules.yml")
	if err := os.WriteFile(name, []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(name, 60001, 60001); err != nil {
		t.Fatal(err)
	}
	if err := checkPaths(dir, "", "", false); err != nil {
		t.Fatal("watch-only requires read/traverse, not replacement rights", err)
	}
}

func TestServingGID(t *testing.T) {
	for _, tc := range []struct {
		name, value   string
		configured    bool
		current, want int
		invalid       bool
	}{
		{name: "default", want: 2000},
		{name: "explicit zero", value: "0", configured: true, want: 0},
		{name: "explicit bootstrap group", value: "4321", configured: true, want: 4321},
		{name: "runtime group wins", value: "0", configured: true, current: 1234, want: 1234},
		{name: "empty", configured: true, invalid: true},
		{name: "negative", value: "-1", configured: true, invalid: true},
		{name: "reserved", value: "4294967295", configured: true, invalid: true},
		{name: "overflow", value: "4294967296", configured: true, invalid: true},
		{name: "text", value: "root", configured: true, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RELOADER_GID", tc.value)
			if !tc.configured {
				if err := os.Unsetenv("RELOADER_GID"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := servingGID(2000, tc.current)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid serving group accepted")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("GID = %d, want %d: %v", got, tc.want, err)
			}
		})
	}
}

func TestDropAndExecPrimaryGroupZero(t *testing.T) {
	if os.Getenv("RELOADER_GROUP_ZERO_TEST_CHILD") == "1" {
		if os.Geteuid() == 0 {
			gid, err := servingGID(2000, os.Getgid())
			if err != nil {
				t.Fatal(err)
			}
			if err := dropAndExec(1000, gid); err != nil {
				t.Fatal(err)
			}
			t.Fatal("exec returned")
		}
		if os.Geteuid() != 1000 || os.Getgid() != 0 || os.Getegid() != 0 {
			t.Fatal("explicit primary GID zero not retained")
		}
		groups, err := os.Getgroups()
		if err != nil {
			t.Fatal(err)
		}
		for _, gid := range groups {
			if gid == 0 {
				t.Fatal("test must not rely on supplementary group zero")
			}
		}
		if got, err := os.ReadFile(os.Getenv("RELOADER_GROUP_ZERO_TEST_FILE")); err != nil || string(got) != "group zero readable" {
			t.Fatalf("lost primary group access: %q %v", got, err)
		}
		if syscall.Setuid(0) == nil {
			t.Fatal("regained root")
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires disposable Linux root runner")
	}
	dir, err := os.MkdirTemp("", "reloader-primary-group-zero-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "group-zero.txt")
	if err := os.WriteFile(file, []byte("group zero readable"), 0040); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(file, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0040); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDropAndExecPrimaryGroupZero$")
	cmd.Env = append(os.Environ(), "RELOADER_GROUP_ZERO_TEST_CHILD=1", "RELOADER_GID=0", "RELOADER_GROUP_ZERO_TEST_FILE="+file)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 0, Gid: 0, Groups: []uint32{1234}}}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

func TestReadOnlyRuleDestinations(t *testing.T) {
	if os.Getenv("RELOADER_READONLY_TEST_CHILD") == "1" {
		// Make mounts private before creating bind mounts in this child namespace.
		if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		active, data := filepath.Join(root, "active"), filepath.Join(root, "data")
		saved := filepath.Join(data, "latest-rules", "v1")
		for _, dir := range []string{active, saved} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
		}
		for _, dir := range []string{active, saved} {
			t.Run(filepath.Base(dir), func(t *testing.T) {
				if err := syscall.Mount(dir, dir, "", syscall.MS_BIND, ""); err != nil {
					t.Fatal(err)
				}
				defer syscall.Unmount(dir, 0)
				if err := syscall.Mount("", dir, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY, ""); err != nil {
					t.Fatal(err)
				}
				if err := checkPaths(active, data, "v1", false); err != nil {
					t.Fatalf("read-only watcher rejected: %v", err)
				}
				if err := CheckWritable(active, data, "v1"); err == nil || !strings.Contains(err.Error(), "read-only") {
					t.Fatalf("read-only editing was not rejected: %v", err)
				}
			})
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires disposable Linux root runner")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestReadOnlyRuleDestinations$")
	cmd.Env = append(os.Environ(), "RELOADER_READONLY_TEST_CHILD=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated mount test failed: %v: %s", err, out)
	}
}

func TestServingIdentityWithTraverseOnlyAncestors(t *testing.T) {
	if os.Getenv("RELOADER_TRAVERSE_TEST_CHILD") == "1" {
		root := os.Getenv("RELOADER_TRAVERSE_TEST_ROOT")
		active, data := filepath.Join(root, "active"), filepath.Join(root, "data")
		if _, err := os.ReadDir(data); err == nil {
			t.Fatal("test ancestor is readable; expected traverse-only access")
		}
		// Prepare is the first operation after the compatibility bootstrap execs.
		if err := Prepare(active, data, "v1"); err != nil {
			t.Fatalf("serving startup rejected accessible rules: %v", err)
		}
		if err := CheckWritable(active, data, "v1"); err != nil {
			t.Fatalf("editing rejected accessible rules: %v", err)
		}
		for _, dir := range []string{active, filepath.Join(data, "latest-rules", "v1")} {
			if err := os.WriteFile(filepath.Join(dir, "probe.rules.yml"), []byte("groups: []"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires disposable Linux root runner")
	}
	root, err := os.MkdirTemp("", "reloader-traverse-only-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	active, data := filepath.Join(root, "active"), filepath.Join(root, "data")
	saved := filepath.Join(data, "latest-rules", "v1")
	for _, dir := range []string{active, saved} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{active, saved} {
		if err := os.Chown(dir, 1000, 2000); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0750); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{data, filepath.Join(data, "latest-rules")} {
		if err := os.Chmod(dir, 0711); err != nil {
			t.Fatal(err)
		}
	}
	// The copied executable avoids root-only Go build-cache ancestors.
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "identity.test")
	if err := os.WriteFile(child, binary, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(child, "-test.run=^TestServingIdentityWithTraverseOnlyAncestors$")
	cmd.Env = append(os.Environ(), "RELOADER_TRAVERSE_TEST_CHILD=1", "RELOADER_TRAVERSE_TEST_ROOT="+root)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 1000, Gid: 2000, Groups: []uint32{2000}}}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("serving identity failed: %v: %s", err, out)
	}
}
