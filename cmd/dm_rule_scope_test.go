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

// The operator initializer is shared by TiDB and DM monitors. DM rules must only
// be installed or restored by the DM initializer; a TiDB-only monitor must never
// end up loading them.
var operatorInitializerSources = []string{
	"../platform-monitoring/operator/init.sh",
	"../monitor-snapshot/master/operator/init.sh",
}

func TestTiDBInitializerDoesNotInstallOrRestoreDMRules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("initializer requires a POSIX shell")
	}
	for _, source := range operatorInitializerSources {
		t.Run(source, func(t *testing.T) {
			root := initializerTempDir(t)
			assets := filepath.Join(root, "assets")
			if err := os.MkdirAll(assets, 0755); err != nil {
				t.Fatal(err)
			}
			script, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			// Create every dashboard/datasource referenced by the selected
			// initializer so the test exercises the real script, not a subset.
			for _, match := range regexp.MustCompile(`/tmp/([A-Za-z0-9_.-]+)`).FindAllStringSubmatch(string(script), -1) {
				name := match[1]
				if filepath.Ext(name) != ".json" && filepath.Ext(name) != ".yaml" {
					continue
				}
				if err := os.WriteFile(filepath.Join(assets, name), []byte("{}\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			for name, body := range map[string]string{
				"tidb.rules.yml":      "groups: []\n# tidb bundled\n",
				"dm_worker.rules.yml": "groups: []\n# dm bundled\n",
			} {
				if err := os.WriteFile(filepath.Join(assets, name), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}

			output := filepath.Join(root, "prometheus")
			rules := filepath.Join(output, "rules")
			persistent := filepath.Join(root, "persistent")
			store := filepath.Join(persistent, "latest-rules", "tidb:v1")
			for _, dir := range []string{rules, store} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			// Simulate both a previous buggy install and a persisted DM rule.
			for _, path := range []string{
				filepath.Join(rules, "dm_worker.rules.yml"),
				filepath.Join(store, "dm_worker.rules.yml"),
			} {
				if err := os.WriteFile(path, []byte("groups: []\n# dm legacy\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(store, "tidb.rules.yml"), []byte("groups: []\n# tidb saved\n"), 0644); err != nil {
				t.Fatal(err)
			}

			path := os.Getenv("PATH")
			if runtime.GOOS == "darwin" {
				gsed, err := exec.LookPath("gsed")
				if err != nil {
					t.Skip("GNU sed is required to exercise the BusyBox initializer on macOS")
				}
				bin := filepath.Join(root, "bin")
				if err := os.MkdirAll(bin, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(gsed, filepath.Join(bin, "sed")); err != nil {
					t.Fatal(err)
				}
				path = bin + string(os.PathListSeparator) + path
			}
			scriptPath := filepath.Join(root, "init.sh")
			if err := os.WriteFile(scriptPath, []byte(strings.ReplaceAll(string(script), "/tmp/", assets+"/")), 0644); err != nil {
				t.Fatal(err)
			}
			baseEnv := []string{
				"PATH=" + path,
				"GF_PROVISIONING_PATH=" + filepath.Join(root, "grafana"),
				"PROM_CONFIG_PATH=" + output,
				"PROM_PERSISTENT_DIR=" + persistent,
				"TIDB_CLUSTER_NAME=test",
				"TIDB_VERSION=tidb:v1",
				"DM_VERSION=tidb:v1",
				"GF_DATASOURCE_PATH=",
				fmt.Sprintf("RELOADER_UID=%d", os.Geteuid()),
				fmt.Sprintf("RELOADER_GID=%d", os.Getegid()),
			}

			run := func(dmCluster string) {
				t.Helper()
				cmd := exec.Command("sh", scriptPath)
				cmd.Env = append(os.Environ(), baseEnv...)
				cmd.Env = append(cmd.Env, "DM_CLUSTER_NAME="+dmCluster)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("initializer failed: %v %s", err, out)
				}
			}

			run("")
			if _, err := os.Stat(filepath.Join(rules, "dm_worker.rules.yml")); !os.IsNotExist(err) {
				t.Fatalf("TiDB-only initializer installed/restored DM rules: %v", err)
			}
			if _, err := os.Stat(filepath.Join(rules, "tidb.rules.yml")); err != nil {
				t.Fatalf("TiDB rules were not restored: %v", err)
			}

			// The DM initializer runs after the TiDB initializer against the
			// same directory and must still install/restore the DM rules.
			run("test")
			if _, err := os.Stat(filepath.Join(rules, "dm_worker.rules.yml")); err != nil {
				t.Fatalf("DM initializer did not install DM rules: %v", err)
			}
			if _, err := os.Stat(filepath.Join(rules, "tidb.rules.yml")); err != nil {
				t.Fatalf("DM initializer removed TiDB rules: %v", err)
			}
		})
	}
}
