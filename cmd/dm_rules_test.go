package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

type dmTransport func(*http.Request) (*http.Response, error)

func TestInitializerPreparesRuleAccess(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership preparation requires a disposable root container")
	}
	if runtime.GOOS == "windows" {
		t.Skip("initializer requires POSIX permissions")
	}
	for _, source := range []string{"init.sh", "../platform-monitoring/operator/init.sh", "../monitor-snapshot/release-8.5/operator/init.sh"} {
		t.Run(source, func(t *testing.T) {
			script, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			prefix, _, ok := strings.Cut(string(script), "# Multiline replacements")
			if !ok || !strings.Contains(prefix, "prepare_reloader_rules()") {
				t.Fatal("initializer does not prepare reloader access")
			}
			dir := initializerTempDir(t)
			active := filepath.Join(dir, "active rules", "rules")
			data := filepath.Join(dir, "data")
			store := filepath.Join(data, "latest-rules", "tidb:test")
			for _, path := range []string{active, store} {
				if err := os.MkdirAll(path, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "test.yml"), []byte("groups: []"), 0444); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(path, "test.yml"), 0444); err != nil {
					t.Fatal(err)
				}
			}
			unrelated := filepath.Join(data, "prometheus")
			if err := os.Mkdir(unrelated, 0700); err != nil {
				t.Fatal(err)
			}
			run := func() error {
				cmd := exec.Command("sh", "-c", prefix+"\nprepare_reloader_rules tidb:test")
				cmd.Env = append(os.Environ(), "PROM_CONFIG_PATH="+filepath.Dir(active), "PROM_PERSISTENT_DIR="+data,
					fmt.Sprintf("RELOADER_UID=%d", os.Geteuid()), fmt.Sprintf("RELOADER_GID=%d", os.Getegid()))
				output, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("%w: %s", err, output)
				}
				return nil
			}
			if err := run(); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{active, store} {
				info, err := os.Stat(filepath.Join(path, "test.yml"))
				if err != nil || info.Mode().Perm() != 0644 {
					t.Fatalf("rule permissions: %v, %v", info, err)
				}
			}
			info, err := os.Stat(unrelated)
			if err != nil || info.Mode().Perm() != 0700 {
				t.Fatal("unrelated data directory permissions changed", err)
			}
			outside := filepath.Join(dir, "outside.yml")
			if err := os.WriteFile(outside, []byte("private"), 0400); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(active, "alias.yml")); err != nil {
				t.Fatal(err)
			}
			if err := run(); err == nil {
				t.Fatal("symbolic-link rule accepted")
			}
			info, err = os.Stat(outside)
			if err != nil || info.Mode().Perm() != 0400 {
				t.Fatal("permissions changed through symlink", err)
			}
		})
	}
}

func (f dmTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDMRulesAreGenerated(t *testing.T) {
	body, err := os.ReadFile("testdata/dm_worker.rules.yml")
	if err != nil {
		t.Fatal(err)
	}
	oldConfigFile, oldOutputDir, oldTag, oldAutoPush := configFile, outputDir, tag, autoPush
	oldTransport, oldConfig, oldUseGlobalTag, oldBaseTagDir := http.DefaultTransport, cfg, useGlobalTag, baseTagDir
	oldAnsibleDashboards, oldAnsibleRules := ansibleGrafanaDir, ansibleRuleDir
	oldOperatorDashboards, oldOperatorRules, oldExpr := operatorGrafanaDir, operatorRuleDir, operatorReplaceExpr
	t.Cleanup(func() {
		configFile, outputDir, tag, autoPush = oldConfigFile, oldOutputDir, oldTag, oldAutoPush
		http.DefaultTransport, cfg, useGlobalTag, baseTagDir = oldTransport, oldConfig, oldUseGlobalTag, oldBaseTagDir
		ansibleGrafanaDir, ansibleRuleDir = oldAnsibleDashboards, oldAnsibleRules
		operatorGrafanaDir, operatorRuleDir, operatorReplaceExpr = oldOperatorDashboards, oldOperatorRules, oldExpr
	})
	configFile, outputDir, tag, autoPush = "../monitoring.yaml", t.TempDir(), "release-8.5", false
	operatorReplaceExpr = make(map[string]string)
	if err := stepUp(); err != nil {
		t.Fatal(err)
	}

	const (
		rawURL    = "https://raw.githubusercontent.com"
		rule      = "/pingcap/tiflow/release-8.5/dm/metrics/alertmanager/dm_worker.rules.yml"
		dashboard = "/pingcap/tiflow/release-8.5/metrics/grafana/TiCDC-Monitor-Summary.json"
		readme    = "/pingcap/tiflow/release-8.5/README.md"
	)
	directory := func(path string) string {
		data, err := json.Marshal([]map[string]string{{"name": filepath.Base(path), "download_url": rawURL + path}})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	rootFetched := false
	http.DefaultTransport = dmTransport(func(r *http.Request) (*http.Response, error) {
		response := "[]"
		switch r.URL.Path {
		case "/repos/pingcap/tiflow/contents/", "/repos/pingcap/ticdc/contents/":
			rootFetched = true
			response = directory(readme)
		case "/repos/pingcap/tiflow/contents/dm/metrics/alertmanager":
			response = directory(rule)
		case "/repos/pingcap/tiflow/contents/metrics/grafana":
			response = directory(dashboard)
		case rule:
			response = string(body)
		case dashboard:
			response = `{"title":"Test-Cluster-TiCDC-Summary"}`
		case readme:
			response = "# TiFlow"
		default:
			if strings.HasSuffix(r.URL.Path, "/metrics/grafana") {
				response = directory(dashboard)
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	if err := Start(); err != nil {
		t.Fatal(err)
	}
	if rootFetched {
		t.Error("a component without an asset path fetched the repository root")
	}

	file, err := os.Open(filepath.Join(baseTagDir, "ansible-monitor.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	archive := tar.NewReader(gzipReader)
	var hasDashboard, hasRule bool
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch header.Name {
		case "ansible/tidb-monitor/README.md":
			t.Error("repository README leaked into Ansible dashboards")
		case "ansible/tidb-monitor/TiCDC-Monitor-Summary.json":
			hasDashboard = true
		case "ansible/tidb-rule/dm_worker.rules.yml":
			raw, err := io.ReadAll(archive)
			if err != nil || string(raw) != string(body) {
				t.Fatalf("Ansible rule changed: %v", err)
			}
			hasRule = true
		}
	}
	if !hasDashboard || !hasRule {
		t.Fatalf("missing Ansible artifacts: dashboard=%t, rule=%t", hasDashboard, hasRule)
	}

	generated, err := os.ReadFile(filepath.Join(operatorRuleDir, filepath.Base(rule)))
	if err != nil {
		t.Fatal(err)
	}
	const operatorExpr = `max by (tidb_cluster) (up{component="dm-master"}) == 0`
	if strings.Count(string(generated), "expr: "+operatorExpr) != 2 {
		t.Error("Operator rule did not override both the alert expression and its expr label")
	}
	snapshot, err := os.ReadFile("../monitor-snapshot/release-8.5/operator/rules/dm_worker.rules.yml")
	if err != nil || string(generated) != string(snapshot) {
		t.Fatalf("Operator rule differs from the 8.5 snapshot: %v", err)
	}
}

func TestDMInitializerRejectsMissingRules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("initializer requires a POSIX shell")
	}
	for _, source := range []string{"init.sh", "../platform-monitoring/operator/init.sh", "../monitor-snapshot/release-8.5/operator/init.sh"} {
		t.Run(source, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			assets := filepath.Join(root, "assets")
			extraPath := ""
			if runtime.GOOS == "darwin" {
				// BusyBox/GNU sed -i syntax is used in the deployed image.
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
				extraPath = bin + string(os.PathListSeparator)
			}

			if err := os.MkdirAll(assets, 0755); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"DM-Monitor-Standard.json", "DM-Monitor-Professional.json"} {
				if err := os.WriteFile(filepath.Join(assets, name), []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(assets, "dm-cluster-datasource.yaml"), []byte("url: http://127.0.0.1:9090\n"), 0644); err != nil {
				t.Fatal(err)
			}
			datasource := filepath.Join(root, "data sources")
			if err := os.MkdirAll(datasource, 0755); err != nil {
				t.Fatal(err)
			}
			script, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			scriptPath := filepath.Join(root, "init.sh")
			if err := os.WriteFile(scriptPath, []byte(strings.ReplaceAll(string(script), "/tmp/", assets+"/")), 0644); err != nil {
				t.Fatal(err)
			}
			for _, hasRules := range []bool{false, true} {
				if hasRules {
					body, err := os.ReadFile("testdata/dm_worker.rules.yml")
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(assets, "dm_worker.rules.yml"), body, 0644); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command("sh", scriptPath)
				cmd.Env = append(os.Environ(), fmt.Sprintf("RELOADER_UID=%d", os.Geteuid()), fmt.Sprintf("RELOADER_GID=%d", os.Getegid()), "DM_CLUSTER_NAME=test", "GF_PROVISIONING_PATH="+filepath.Join(root, "grafana space"), "PROM_CONFIG_PATH="+filepath.Join(root, "prometheus space"), "PROM_PERSISTENT_DIR=", "GF_DATASOURCE_PATH="+datasource, "GF_DM_PROMETHEUS_URL=http://example.test/?a=1&b=2", "PATH="+extraPath+os.Getenv("PATH"))
				out, err := cmd.CombinedOutput()
				if !hasRules && err == nil {
					t.Errorf("missing rules reported success: %s", out)
				}
				if hasRules && err != nil {
					t.Fatalf("valid DM initializer failed: %v %s", err, out)
				}
				if hasRules {
					got, err := os.ReadFile(filepath.Join(datasource, "dm-cluster-datasource.yaml"))
					if err != nil || string(got) != "url: http://example.test/?a=1&b=2\n" {
						t.Errorf("URL replacement corrupted: %q %v", got, err)
					}
				}

			}
		})
	}
}

func TestInitializerRejectsMultilineReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("initializer requires a POSIX shell")
	}
	for _, source := range []string{"init.sh", "../platform-monitoring/operator/init.sh", "../monitor-snapshot/release-8.5/operator/init.sh"} {
		t.Run(source, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", source)
			cmd.Env = append(os.Environ(), fmt.Sprintf("RELOADER_UID=%d", os.Geteuid()), fmt.Sprintf("RELOADER_GID=%d", os.Getegid()), "DM_CLUSTER_NAME=test", "GF_PROVISIONING_PATH="+filepath.Join(root, "grafana"), "PROM_CONFIG_PATH="+filepath.Join(root, "prometheus"), "GF_DM_PROMETHEUS_URL=bad\nreplacement")
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "must be single-line") {
				t.Fatalf("multiline value not rejected: %v %s", err, out)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("initializer changed output before rejecting input")
			}
		})
	}
}

func TestTiDBInitializerPreservesDatasourceURLs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("initializer requires a POSIX shell")
	}
	for _, source := range []string{"init.sh", "../platform-monitoring/operator/init.sh", "../monitor-snapshot/release-8.5/operator/init.sh"} {
		t.Run(source, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			assets := filepath.Join(root, "assets")
			datasource := filepath.Join(root, "data sources")
			for _, dir := range []string{assets, datasource} {
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
				if err := os.MkdirAll(bin, 0755); err != nil {
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
				body := `{"title":"Test-Cluster-TiDB"}`
				switch filepath.Ext(name) {
				case ".json":
				case ".yml":
					body = "groups: []"
				case ".yaml":
					body = "url: http://127.0.0.1:9090\n"
					if name == "k8s-datasource.yaml" {
						body = "url: http://prometheus-k8s.monitoring.svc:9090\n"
					}
				default:
					continue
				}
				if err := os.WriteFile(filepath.Join(assets, name), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(assets, "test.rules.yml"), []byte("groups: []"), 0644); err != nil {
				t.Fatal(err)
			}
			persistent := filepath.Join(root, "persistent")
			saved := filepath.Join(persistent, "latest-rules", "v8.5.9")
			if err := os.MkdirAll(saved, 0755); err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{assets, saved} {
				if err := os.WriteFile(filepath.Join(dir, "dm_worker.rules.yml"), []byte("groups: []"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(saved, "custom.rules.yml"), []byte("groups: []"), 0644); err != nil {
				t.Fatal(err)
			}
			scriptPath := filepath.Join(root, "init.sh")
			if err := os.WriteFile(scriptPath, []byte(strings.ReplaceAll(string(script), "/tmp/", assets+"/")), 0644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", scriptPath)
			cmd.Dir = root
			k8sURL := "http://example.test/?a=1&b=2,tail"
			tidbURL := "http://example.test/tidb?a=1&b=2"
			cmd.Env = append(os.Environ(), fmt.Sprintf("RELOADER_UID=%d", os.Geteuid()), fmt.Sprintf("RELOADER_GID=%d", os.Getegid()), "DM_CLUSTER_NAME=", "TIDB_CLUSTER_NAME=test", "TIDB_CLUSTER_NAMESPACE=default", "GF_PROVISIONING_PATH="+filepath.Join(root, "grafana space"), "PROM_CONFIG_PATH="+filepath.Join(root, "prometheus space"), "PROM_PERSISTENT_DIR="+persistent, "TIDB_VERSION=v8.5.9", "GF_DATASOURCE_PATH="+datasource, "GF_K8S_PROMETHEUS_URL="+k8sURL, "GF_TIDB_PROMETHEUS_URL="+tidbURL, "PATH="+extraPath+os.Getenv("PATH"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("TiDB initializer failed: %v %s", err, out)
			}
			promRules := filepath.Join(root, "prometheus space", "rules")
			if _, err := os.Stat(filepath.Join(promRules, "dm_worker.rules.yml")); !os.IsNotExist(err) {
				t.Fatalf("TiDB initializer installed DM rules: stat error %v", err)
			}
			if _, err := os.Stat(filepath.Join(promRules, "custom.rules.yml")); err != nil {
				t.Fatalf("TiDB initializer did not restore persisted rules: %v", err)
			}
			for name, url := range map[string]string{"k8s-datasource.yaml": k8sURL, "tidb-cluster-datasource.yaml": tidbURL} {
				got, err := os.ReadFile(filepath.Join(datasource, name))
				if err != nil || string(got) != "url: "+url+"\n" {
					t.Fatalf("URL replacement corrupted: %q %v", got, err)
				}
			}
		})
	}
}

func initializerTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
