//go:build linux

package identity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPreparePaths(t *testing.T) {
	for _, attack := range []string{"none", "symlink-file", "hardlink-file", "symlink-dir", "symlink-parent", "fifo", "version"} {
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
			case "version":
				version = "../other"
			default:
				os.WriteFile(rule, []byte("groups: []"), 0444)
			}
			err := preparePaths(active, data, version, os.Geteuid(), os.Getegid())
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
			if err := dropAndExec(1000, 2000); err != nil {
				t.Fatal(err)
			}
			t.Fatal("exec returned")
		}
		if os.Getuid() != 1000 || os.Geteuid() != 1000 || os.Getgid() != 2000 || os.Getegid() != 2000 {
			t.Fatal("wrong runtime identity")
		}
		if err := Prepare("/does-not-exist", "/does-not-exist", "invalid"); err != nil {
			t.Fatal("non-root runtime attempted ownership preparation", err)
		}
		groups, err := os.Getgroups()
		if err != nil || len(groups) != 0 {
			t.Fatal("supplementary groups retained", groups, err)
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
