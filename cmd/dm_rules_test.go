package main

import (
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

	"github.com/pingcap/monitoring/pkg/common"
	"gopkg.in/yaml.v3"
)

type dmTransport func(*http.Request) (*http.Response, error)

func TestInitializerPreparesRuleAccess(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership preparation requires a disposable root container")
	}
	if runtime.GOOS == "windows" {
		t.Skip("initializer requires POSIX permissions")
	}
	for _, source := range []string{"init.sh", "../platform-monitoring/operator/init.sh", "../monitor-snapshot/master/operator/init.sh"} {
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
	data, err := os.ReadFile("../monitoring.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, component := range config.ComponentConfigs {
		if component.RepoName == "tiflow" && component.RulesPath == "dm/metrics/alertmanager" {
			found = true
		}
	}
	if !found {
		t.Fatal("default generator config does not fetch DM rules")
	}
	body, err := os.ReadFile("testdata/dm_worker.rules.yml")
	if err != nil {
		t.Fatal(err)
	}
	oldTransport, oldAnsible, oldOperator := http.DefaultTransport, ansibleRuleDir, operatorRuleDir
	t.Cleanup(func() { http.DefaultTransport, ansibleRuleDir, operatorRuleDir = oldTransport, oldAnsible, oldOperator })
	ansibleRuleDir, operatorRuleDir = initializerTempDir(t), initializerTempDir(t)
	http.DefaultTransport = dmTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	service, err := common.NewGitRepoService()
	if err != nil {
		t.Fatal(err)
	}
	name, target := "dm_worker.rules.yml", "https://raw.githubusercontent.com/pingcap/tiflow/master/dm/metrics/alertmanager/dm_worker.rules.yml"
	ProcessRules([]*common.RepositoryContent{{Name: &name, DownloadURL: &target}}, service)
	for _, dir := range []string{ansibleRuleDir, operatorRuleDir} {
		generated, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(generated) == 0 {
			t.Fatalf("DM rule missing: %s %v", dir, err)
		}
	}
}

func TestDMInitializerRejectsMissingRules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("initializer requires a POSIX shell")
	}
	for _, source := range []string{"init.sh", "../platform-monitoring/operator/init.sh", "../monitor-snapshot/master/operator/init.sh"} {
		t.Run(source, func(t *testing.T) {
			root := initializerTempDir(t)
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
	for _, source := range []string{"init.sh", "../platform-monitoring/operator/init.sh", "../monitor-snapshot/master/operator/init.sh"} {
		t.Run(source, func(t *testing.T) {
			root := initializerTempDir(t)
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
	for _, source := range []string{"init.sh", "../platform-monitoring/operator/init.sh", "../monitor-snapshot/master/operator/init.sh"} {
		t.Run(source, func(t *testing.T) {
			root := initializerTempDir(t)
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
			scriptPath := filepath.Join(root, "init.sh")
			if err := os.WriteFile(scriptPath, []byte(strings.ReplaceAll(string(script), "/tmp/", assets+"/")), 0644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", scriptPath)
			cmd.Dir = root
			k8sURL := "http://example.test/?a=1&b=2,tail"
			tidbURL := "http://example.test/tidb?a=1&b=2"
			cmd.Env = append(os.Environ(), fmt.Sprintf("RELOADER_UID=%d", os.Geteuid()), fmt.Sprintf("RELOADER_GID=%d", os.Getegid()), "DM_CLUSTER_NAME=", "TIDB_CLUSTER_NAME=test", "TIDB_CLUSTER_NAMESPACE=default", "GF_PROVISIONING_PATH="+filepath.Join(root, "grafana space"), "PROM_CONFIG_PATH="+filepath.Join(root, "prometheus space"), "PROM_PERSISTENT_DIR=", "GF_DATASOURCE_PATH="+datasource, "GF_K8S_PROMETHEUS_URL="+k8sURL, "GF_TIDB_PROMETHEUS_URL="+tidbURL, "PATH="+extraPath+os.Getenv("PATH"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("TiDB initializer failed: %v %s", err, out)
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
