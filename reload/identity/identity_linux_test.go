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
	opened, err := openDirectory(dir, false)
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
	opened, err := openDirectory(dir, false)
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
