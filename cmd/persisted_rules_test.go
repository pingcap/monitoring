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
			for _, scenario := range []string{"default", "custom-name", "hidden-name", "symlink", "hardlink", "directory-link", "invalid-version", "config-link", "config-parent-link", "active-symlink", "active-hardlink", "restore-destination-symlink"} {
				t.Run(source+"/"+map[bool]string{false: "tidb", true: "dm"}[dm]+"/"+scenario, func(t *testing.T) {
					if !dm && (scenario == "active-symlink" || scenario == "active-hardlink" || scenario == "restore-destination-symlink") {
						t.Skip("DM shares an existing active directory")
					}
					root := initializerTempDir(t)
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
					outside := filepath.Join(root, "outside.yml")
					if err := os.WriteFile(outside, []byte("unchanged target"), 0644); err != nil {
						t.Fatal(err)
					}
					switch scenario {
					case "active-symlink":
						if err := os.Symlink(outside, filepath.Join(output, "rules", name)); err != nil {
							t.Fatal(err)
						}
					case "active-hardlink":
						if err := os.Link(outside, filepath.Join(output, "rules", name)); err != nil {
							t.Fatal(err)
						}
					case "restore-destination-symlink":
						if err := os.WriteFile(filepath.Join(filepath.Dir(saved), "custom.rules.yml"), []byte(restored), 0644); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(outside, filepath.Join(output, "rules", "custom.rules.yml")); err != nil {
							t.Fatal(err)
						}
					case "config-link":
						actual := filepath.Join(root, "actual-prometheus")
						if err := os.Rename(output, actual); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(actual, output); err != nil {
							t.Fatal(err)
						}
					case "config-parent-link":
						link := filepath.Join(root, "link")
						if err := os.Symlink(root, link); err != nil {
							t.Fatal(err)
						}
						output = filepath.Join(link, "prometheus")
					case "hidden-name":
						for _, hidden := range []string{".custom.rules.yml", ".rules.yml"} {
							if err := os.WriteFile(filepath.Join(filepath.Dir(saved), hidden), []byte(restored), 0644); err != nil {
								t.Fatal(err)
							}
						}
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
					success := scenario == "default" || scenario == "custom-name" || scenario == "hidden-name"
					if success && err != nil {
						t.Fatalf("initializer failed: %v %s", err, out)
					}
					if !success && err == nil {
						t.Fatalf("unsafe restore succeeded: %s", out)
					}
					if !success {
						if scenario == "config-link" || scenario == "config-parent-link" {
							if _, err := os.Stat(old); err != nil {
								t.Fatal("deleted files through a symlink ancestor", err)
							}
						}
						got, readErr := os.ReadFile(outside)
						if readErr != nil || string(got) != "unchanged target" {
							t.Fatalf("outside target changed: %q %v", got, readErr)
						}
						return
					}
					want := restored
					got, err := os.ReadFile(filepath.Join(output, "rules", name))
					if err != nil || string(got) != want {
						t.Fatalf("rules = %q, want %q: %v", got, want, err)
					}
					if scenario == "hidden-name" {
						for _, hidden := range []string{".custom.rules.yml", ".rules.yml"} {
							got, err := os.ReadFile(filepath.Join(output, "rules", hidden))
							if err != nil || string(got) != restored {
								t.Fatalf("hidden rule not restored: %s %v", hidden, err)
							}
						}
					}
					if scenario == "custom-name" {
						custom, err := os.ReadFile(filepath.Join(output, "rules", "foreign.rules.yml"))
						if err != nil || string(custom) != restored {
							t.Fatal("custom rule was not restored", err)
						}
					}
					if _, err := os.Lstat(old); dm && err != nil || !dm && !os.IsNotExist(err) {
						t.Fatalf("unexpected shared rules cleanup: %v", err)
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

// Operator runs these initializers in sequence, sharing the rules emptyDir/PVC.
func TestTiDBAndDMInitializeSharedRules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("initializer requires POSIX shell")
	}
	for _, source := range []string{"init.sh", "../platform-monitoring/operator/init.sh", "../monitor-snapshot/master/operator/init.sh"} {
		for _, persisted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/persisted=%t", source, persisted), func(t *testing.T) {
				root := initializerTempDir(t)
				output, data := filepath.Join(root, "prometheus"), filepath.Join(root, "persistent")
				store := filepath.Join(data, "latest-rules", "tidb:v1")
				if err := os.MkdirAll(store, 0755); err != nil {
					t.Fatal(err)
				}
				script, err := os.ReadFile(source)
				if err != nil {
					t.Fatal(err)
				}
				path := os.Getenv("PATH")
				if runtime.GOOS == "darwin" {
					sed, err := exec.LookPath("gsed")
					if err != nil {
						t.Skip("GNU sed required on macOS")
					}
					bin := filepath.Join(root, "bin")
					if err := os.Mkdir(bin, 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(sed, filepath.Join(bin, "sed")); err != nil {
						t.Fatal(err)
					}
					path = bin + string(os.PathListSeparator) + path
				}
				expected, runs := map[string]string{}, map[string]string{}
				for _, stage := range []string{"tidb", "dm"} {
					assets := filepath.Join(root, stage+"-assets")
					if err := os.Mkdir(assets, 0755); err != nil {
						t.Fatal(err)
					}
					for _, match := range regexp.MustCompile(`/tmp/([A-Za-z0-9_.-]+)`).FindAllStringSubmatch(string(script), -1) {
						name := match[1]
						if filepath.Ext(name) != ".json" && filepath.Ext(name) != ".yaml" {
							continue
						}
						if err := os.WriteFile(filepath.Join(assets, name), []byte("{}\n"), 0644); err != nil {
							t.Fatal(err)
						}
					}
					name := "tidb.rules.yml"
					if stage == "dm" {
						name = "dm_worker.rules.yml"
					}
					bundled := "groups: []\n# " + stage + " bundled\n"
					if err := os.WriteFile(filepath.Join(assets, name), []byte(bundled), 0644); err != nil {
						t.Fatal(err)
					}
					expected[name] = bundled
					if persisted {
						expected[name] = "groups: []\n# " + stage + " saved\n"
						if err := os.WriteFile(filepath.Join(store, name), []byte(expected[name]), 0644); err != nil {
							t.Fatal(err)
						}
					}
					runs[stage] = filepath.Join(root, stage+".sh")
					if err := os.WriteFile(runs[stage], []byte(strings.ReplaceAll(string(script), "/tmp/", assets+"/")), 0644); err != nil {
						t.Fatal(err)
					}
				}
				for round := 0; round < 2; round++ {
					if round == 1 {
						// Model edits saved to the PVC, followed by a new Pod's emptyDir.
						for name := range expected {
							expected[name] = "groups: []\n# edited " + name + "\n"
							for _, dir := range []string{filepath.Join(output, "rules"), store} {
								if err := os.WriteFile(filepath.Join(dir, name), []byte(expected[name]), 0644); err != nil {
									t.Fatal(err)
								}
							}
						}
						if err := os.RemoveAll(output); err != nil {
							t.Fatal(err)
						}
					}
					for _, stage := range []string{"tidb", "dm"} {
						cluster := ""
						if stage == "dm" {
							cluster = "test"
						}
						cmd := exec.Command("sh", runs[stage])
						cmd.Env = append(os.Environ(), "PATH="+path, "GF_PROVISIONING_PATH="+filepath.Join(root, "grafana", stage), "PROM_CONFIG_PATH="+output, "PROM_PERSISTENT_DIR="+data, "TIDB_CLUSTER_NAME=test", "DM_CLUSTER_NAME="+cluster, "TIDB_VERSION=tidb:v1", "DM_VERSION=tidb:v1", "GF_DATASOURCE_PATH=", fmt.Sprintf("RELOADER_UID=%d", os.Geteuid()), fmt.Sprintf("RELOADER_GID=%d", os.Getegid()))
						if out, err := cmd.CombinedOutput(); err != nil {
							t.Fatalf("round %d %s: %v %s", round, stage, err, out)
						}
					}
					for name, want := range expected {
						got, err := os.ReadFile(filepath.Join(output, "rules", name))
						if err != nil || string(got) != want {
							t.Fatalf("%s = %q, want %q: %v", name, got, want, err)
						}
						if persisted || round == 1 {
							got, err = os.ReadFile(filepath.Join(store, name))
							if err != nil || string(got) != want {
								t.Fatalf("saved %s changed: %q %v", name, got, err)
							}
						}
					}
				}
			})
		}
	}
}
