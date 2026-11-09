package scripts

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"
)

func TestLegacyInitializerRuleStrings(t *testing.T) {
	releases, err := filepath.Glob("../monitor/v*/init.sh")
	if err != nil || len(releases) != 36 {
		t.Fatalf("release scripts: %d, %v", len(releases), err)
	}
	extraPath := ""
	if runtime.GOOS == "darwin" {
		sed, err := exec.LookPath("gsed")
		if err != nil {
			t.Skip("GNU sed is required on macOS")
		}
		bin := t.TempDir()
		if err := os.Symlink(sed, filepath.Join(bin, "sed")); err != nil {
			t.Fatal(err)
		}
		extraPath = bin + string(os.PathListSeparator)
	}
	for _, script := range releases {
		names := []string{"test-cluster", "&"}
		if filepath.Base(filepath.Dir(script)) == "v3.0.0" {
			names = append(names, "team'qa", "region: qa", "*alias", "#comment", "true", "null", "123", "", "ENV_LABELS_ENV", `region\qa`, `region"qa`, "区域", "control\t\x01\x7f", "unicode\u0085\u009f\u2028\u2029", "description: 'cluster: ENV_LABELS_ENV")
		}
		for _, name := range names {
			t.Run(filepath.Base(filepath.Dir(script))+"/"+name, func(t *testing.T) {
				root, err := os.MkdirTemp("", "legacy-rules-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(root) })
				assets := filepath.Join(root, "assets")
				if err := os.Mkdir(assets, 0755); err != nil {
					t.Fatal(err)
				}
				for _, kind := range []string{"dashboards", "rules", "datasources"} {
					files, err := filepath.Glob(filepath.Join(filepath.Dir(script), kind, "*"))
					if err != nil {
						t.Fatal(err)
					}
					for _, file := range files {
						data, err := os.ReadFile(file)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(assets, filepath.Base(file)), data, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				data, err := os.ReadFile(script)
				if err != nil {
					t.Fatal(err)
				}
				run := filepath.Join(root, "init.sh")
				if err := os.WriteFile(run, []byte(strings.ReplaceAll(string(data), "/tmp/", assets+"/")), 0600); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("sh", run)
				cmd.Env = append(os.Environ(), "PATH="+extraPath+os.Getenv("PATH"), "TIDB_CLUSTER_NAME="+name,
					"GF_PROVISIONING_PATH="+filepath.Join(root, "grafana"), "PROM_CONFIG_PATH="+filepath.Join(root, "prometheus"),
					"GF_DATASOURCE_PATH=", "PROM_PERSISTENT_DIR=", "GF_K8S_PROMETHEUS_URL=", "GF_TIDB_PROMETHEUS_URL=", "TIDB_CLUSTER_NAMESPACE=")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("initializer: %v\n%s", err, out)
				}
				rules, _ := filepath.Glob(filepath.Join(filepath.Dir(script), "rules", "*.rules.yml"))
				for _, file := range rules {
					originalData, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					generated := filepath.Join(root, "prometheus", "rules", filepath.Base(file))
					actualData, err := os.ReadFile(generated)
					if err != nil {
						t.Fatal(err)
					}
					var want, got map[string]any
					if err := yaml.Unmarshal(originalData, &want); err != nil {
						t.Fatal(err)
					}
					if err := yaml.Unmarshal(actualData, &got); err != nil {
						t.Fatalf("%s: %v", file, err)
					}
					for gi, g := range want["groups"].([]any) {
						actualRules := got["groups"].([]any)[gi].(map[string]any)["rules"].([]any)
						for ri, r := range g.(map[string]any)["rules"].([]any) {
							rule := r.(map[string]any)
							rule["labels"].(map[string]any)["env"] = name
							annotations := rule["annotations"].(map[string]any)
							description := annotations["description"].(string)
							actualDescription := actualRules[ri].(map[string]any)["annotations"].(map[string]any)["description"].(string)
							wantRendered := strings.ReplaceAll(renderRuleDescription(t, description, name), "ENV_LABELS_ENV", name)
							if actual := renderRuleDescription(t, actualDescription, name); actual != wantRendered {
								t.Fatalf("%s: annotation = %q, want %q", file, actual, wantRendered)
							}
							annotations["description"] = actualDescription
						}
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("%s: generated rules changed beyond env and its description reference", file)
					}
					if promtool := os.Getenv("PROMTOOL"); promtool != "" && filepath.Base(file) == "tidb.rules.yml" {
						if out, err := exec.Command(promtool, "check", "rules", generated).CombinedOutput(); err != nil {
							t.Fatalf("promtool: %v\n%s", err, out)
						}
						if filepath.Base(filepath.Dir(script)) == "v3.0.0" && (name == "test-cluster" || name == "&" || name == "team'qa") {
							checkLegacyAlert(t, promtool, generated, name)
						}
					}
				}
			})
		}
	}
}

func checkLegacyAlert(t *testing.T, promtool, rules, name string) {
	t.Helper()
	data, err := yaml.Marshal(map[string]any{
		"rule_files": []string{rules}, "evaluation_interval": "1m",
		"tests": []any{map[string]any{
			"interval": "1m",
			"input_series": []any{map[string]any{
				"series": `tidb_monitor_keep_alive_total{job="tidb",instance="node:4000"}`, "values": "0+0x10",
			}},
			"alert_rule_test": []any{map[string]any{
				"eval_time": "10m", "alertname": "TiDB_monitor_keep_alive",
				"exp_alerts": []any{map[string]any{
					"exp_labels": map[string]string{
						"env": name, "job": "tidb", "instance": "node:4000", "level": "emergency",
						"expr": `increase(tidb_monitor_keep_alive_total{job="tidb"}[10m]) < 100`,
					},
					"exp_annotations": map[string]string{
						"description": "cluster: " + name + ", instance: node:4000, values:0",
						"summary":     "TiDB monitor_keep_alive error", "value": "0",
					},
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(filepath.Dir(rules), "alert-test.yml")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(promtool, "test", "rules", file).CombinedOutput(); err != nil {
		t.Fatalf("promtool alert evaluation: %v\n%s", err, out)
	}
}

func renderRuleDescription(t *testing.T, value, name string) string {
	t.Helper()
	tpl, err := template.New("description").Parse("{{ $labels := .Labels }}{{ $value := .Value }}" + value)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	data := map[string]any{"Labels": map[string]string{"env": "sample-env", "instance": "node:4000", "type": "test"}, "Value": fmt.Sprint(1)}
	if err := tpl.Execute(&out, data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
