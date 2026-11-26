package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type ruleErrorTransport func(*http.Request) (*http.Response, error)

func (f ruleErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGeneratorRejectsInvalidRules(t *testing.T) {
	if os.Getenv("MONITORING_RULE_ERROR_TEST") == "1" {
		http.DefaultTransport = ruleErrorTransport(func(r *http.Request) (*http.Response, error) {
			body := "[]"
			switch r.URL.Path {
			case "/repos/pingcap/tidb/contents/rules":
				body = `[{"name":"test.rules.yml","download_url":"https://raw.githubusercontent.com/pingcap/tidb/release-8.5/rules/test.rules.yml"}]`
			case "/pingcap/tidb/release-8.5/rules/test.rules.yml":
				body = "groups:\n- name: test\n  rules:\n  - alert: TestAlert\n    expr: up == 0\n"
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		os.Args = []string{"load", "--config=" + os.Getenv("MONITORING_RULE_TEST_CONFIG"), "--output-dir=" + os.Getenv("MONITORING_RULE_TEST_OUTPUT"), "--tag=release-8.5"}
		main()
		return
	}
	for _, tc := range []struct {
		name string
		expr string
		ok   bool
	}{
		{"invalid replacement", "up{", false},
		{"valid replacement", "up < 1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			config := filepath.Join(dir, "monitoring.yaml")
			body := fmt.Sprintf("components:\n- repo_name: tidb\n  rule_path: rules\noperator:\n  replace_expr:\n  - rule_name: TestAlert\n    expr: %q\n", tc.expr)
			if err := os.WriteFile(config, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "output")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGeneratorRejectsInvalidRules$")
			cmd.Env = append(os.Environ(), "MONITORING_RULE_ERROR_TEST=1", "MONITORING_RULE_TEST_CONFIG="+config, "MONITORING_RULE_TEST_OUTPUT="+output)
			log, err := cmd.CombinedOutput()
			if ctx.Err() != nil || (err == nil) != tc.ok {
				t.Fatalf("exit error: %v; timeout: %v; want success=%t; output:\n%s", err, ctx.Err(), tc.ok, log)
			}
			bundle := filepath.Join(output, "monitor-snapshot", "release-8.5")
			archive := filepath.Join(bundle, "ansible-monitor.tar.gz")
			rule := filepath.Join(bundle, "operator", "rules", "test.rules.yml")
			if !tc.ok {
				if !strings.Contains(string(log), "test.rules.yml") || !strings.Contains(string(log), "parse error") {
					t.Fatalf("error lacks the affected filename or validation cause: %s", log)
				}
				for _, file := range []string{archive, rule} {
					if _, err := os.Stat(file); !os.IsNotExist(err) {
						t.Fatalf("rejected rules produced %s: %v", file, err)
					}
				}
				return
			}
			if _, err := os.Stat(archive); err != nil {
				t.Fatal(err)
			}
			generated, err := os.ReadFile(rule)
			if err != nil || !strings.Contains(string(generated), "expr: up < 1") {
				t.Fatalf("valid replacement was not generated: %v; %s", err, generated)
			}
		})
	}
}
