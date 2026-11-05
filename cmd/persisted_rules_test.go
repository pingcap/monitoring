package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestInitializerRestoresRulesWithoutOptIn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("initializer requires POSIX shell")
	}
	for _, source := range []string{"init.sh", "../platform-monitoring/operator/init.sh", "../monitor-snapshot/master/operator/init.sh"} {
		for _, dm := range []bool{false, true} {
			for _, scenario := range []string{"default", "custom-name", "symlink", "hardlink", "directory-link", "invalid-version"} {
				t.Run(source+"/"+map[bool]string{false: "tidb", true: "dm"}[dm]+"/"+scenario, func(t *testing.T) {
					root := t.TempDir()
					assets, output, persistent := filepath.Join(root, "assets"), filepath.Join(root, "prometheus"), filepath.Join(root, "persistent")
					for _, dir := range []string{assets, filepath.Join(output, "rules"), filepath.Join(persistent, "latest-rules", "v1")} {
						if err := os.MkdirAll(dir, 0755); err != nil {
							t.Fatal(err)
						}
					}
					extraPath := ""
					if runtime.GOOS == "darwin" {
						gsed, err := exec.LookPath("gsed")
						if err != nil {
							t.Skip("GNU sed required on macOS")
						}
						bin := filepath.Join(root, "bin")
						if err := os.Mkdir(bin, 0755); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(gsed, filepath.Join(bin, "sed")); err != nil {
							t.Fatal(err)
						}
						extraPath = bin + string(os.PathListSeparator)
					}
					script, err := os.ReadFile(source)
					if err != nil {
						t.Fatal(err)
					}
					for _, match := range regexp.MustCompile(`/tmp/([A-Za-z0-9_.-]+)`).FindAllStringSubmatch(string(script), -1) {
						name := match[1]
						if filepath.Ext(name) != ".json" && filepath.Ext(name) != ".yml" && filepath.Ext(name) != ".yaml" {
							continue
						}
						if err := os.WriteFile(filepath.Join(assets, name), []byte("{}"), 0644); err != nil {
							t.Fatal(err)
						}
					}
					name := "test.rules.yml"
					if dm {
						name = "dm_worker.rules.yml"
					}
					bundled, restored := "groups: []\n# bundled\n", "groups: []\n# reviewed custom rules\n"
					if err := os.WriteFile(filepath.Join(assets, name), []byte(bundled), 0644); err != nil {
						t.Fatal(err)
					}
					old := filepath.Join(output, "rules", ".stale.rules.yml")
					if err := os.WriteFile(old, []byte("groups: []"), 0644); err != nil {
						t.Fatal(err)
					}
					saved := filepath.Join(persistent, "latest-rules", "v1", name)
					if err := os.WriteFile(saved, []byte(restored), 0644); err != nil {
						t.Fatal(err)
					}
					switch scenario {
					case "custom-name":
						if err := os.WriteFile(filepath.Join(filepath.Dir(saved), "foreign.rules.yml"), []byte(restored), 0644); err != nil {
							t.Fatal(err)
						}
					case "symlink":
						if err := os.Remove(saved); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(filepath.Join(assets, name), saved); err != nil {
							t.Fatal(err)
						}
					case "directory-link":
						if err := os.Rename(filepath.Dir(saved), filepath.Join(root, "untrusted")); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(filepath.Join(root, "untrusted"), filepath.Dir(saved)); err != nil {
							t.Fatal(err)
						}
					case "hardlink":
						if err := os.Link(saved, filepath.Join(filepath.Dir(saved), "linked.rules.yml")); err != nil {
							t.Fatal(err)
						}
					}
					scriptPath := filepath.Join(root, "init.sh")
					if err := os.WriteFile(scriptPath, []byte(strings.ReplaceAll(string(script), "/tmp/", assets+"/")), 0644); err != nil {
						t.Fatal(err)
					}
					cluster := ""
					if dm {
						cluster = "test"
					}
					cmd := exec.Command("sh", scriptPath)
					cmd.Env = append(os.Environ(), "DM_CLUSTER_NAME="+cluster, "TIDB_CLUSTER_NAME=test", "DM_VERSION="+map[bool]string{true: "..", false: "v1"}[scenario == "invalid-version"], "TIDB_VERSION="+map[bool]string{true: "..", false: "v1"}[scenario == "invalid-version"], "GF_PROVISIONING_PATH="+filepath.Join(root, "grafana"), "PROM_CONFIG_PATH="+output, "PROM_PERSISTENT_DIR="+persistent, fmt.Sprintf("RELOADER_UID=%d", os.Geteuid()), fmt.Sprintf("RELOADER_GID=%d", os.Getegid()), "GF_DATASOURCE_PATH=", "PATH="+extraPath+os.Getenv("PATH"))
					out, err := cmd.CombinedOutput()
					success := scenario == "default" || scenario == "custom-name"
					if success && err != nil {
						t.Fatalf("initializer failed: %v %s", err, out)
					}
					if !success && err == nil {
						t.Fatalf("unsafe restore succeeded: %s", out)
					}
					if !success {
						return
					}
					want := restored
					got, err := os.ReadFile(filepath.Join(output, "rules", name))
					if err != nil || string(got) != want {
						t.Fatalf("rules = %q, want %q: %v", got, want, err)
					}
					if scenario == "custom-name" {
						custom, err := os.ReadFile(filepath.Join(output, "rules", "foreign.rules.yml"))
						if err != nil || string(custom) != restored {
							t.Fatal("custom rule was not restored", err)
						}
					}
					if _, err := os.Lstat(old); !os.IsNotExist(err) {
						t.Fatalf("foreign active rule survived: %v", err)
					}
					got, err = os.ReadFile(saved)
					if err != nil || string(got) != restored {
						t.Fatalf("PVC data was modified: %q %v", got, err)
					}
				})
			}
		}
	}
}
