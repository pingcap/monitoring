package operator

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
)

func TestReleaseRuleSnapshotsMatchTransform(t *testing.T) {
	files, err := filepath.Glob("../../monitor-snapshot/release-8.5/operator/rules/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("release rule snapshots missing: %v", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			body, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			transformed, err := TransformRules(body, nil, false)
			if err != nil || !bytes.Equal(body, transformed) {
				t.Fatalf("release snapshot differs from transformed rules: %v", err)
			}
		})
	}
}

func TestTransformRules(t *testing.T) {
	input := []byte(`groups:
- name: test
  interval: 30s
  limit: 42
  query_offset: 15s
  labels:
    team: test
  rules:
  - alert: ShortAlert
    expr: up == 0
    for: 1m
    labels:
      expr: up == 0
  - alert: LongAlert
    expr: up == 0
    for: 10m
  - record: instance:up
    expr: sum(up)
`)
	output, err := TransformRules(input, map[string]string{"SHORTALERT": "up < 1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	groups, errs := rulefmt.Parse(output, false, model.NameValidationScheme, parser.NewParser(parser.Options{}), slog.Default())
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	g := groups.Groups[0]
	if g.Limit != 42 || g.QueryOffset == nil || time.Duration(*g.QueryOffset) != 15*time.Second || g.Labels["team"] != "test" || time.Duration(g.Interval) != 30*time.Second {
		t.Fatalf("group settings lost: %+v", g)
	}
	if time.Duration(g.Rules[0].For) != 5*time.Minute || g.Rules[0].Expr != "up < 1" || g.Rules[0].Labels["expr"] != "up < 1" {
		t.Fatalf("short alert not transformed: %+v", g.Rules[0])
	}
	if time.Duration(g.Rules[1].For) != 10*time.Minute || g.Rules[2].For != 0 {
		t.Fatal("long alert or recording rule changed")
	}
}

func TestTransformRulesRejectsInvalidReplacement(t *testing.T) {
	output, err := TransformRules([]byte("groups:\n- name: test\n  rules:\n  - alert: TestAlert\n    expr: up == 0\n"), map[string]string{"TESTALERT": "up{"}, false)
	if err == nil || output != nil {
		t.Fatalf("invalid replacement accepted: %s, %v", output, err)
	}
}

func TestOperatorPreservesEventAlertDuration(t *testing.T) {
	const input = `groups:
- name: test
  rules:
  - alert: RelayCorruption
    expr: changes(dm_relay_data_corruption[1m]) > 0
  - alert: ShortAlert
    expr: up == 0
    for: 30s
`
	dir := t.TempDir()
	if err := WriteRule(input, "events.rules.yml", dir, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "events.rules.yml"))
	if err != nil {
		t.Fatal(err)
	}
	groups, errs := rulefmt.Parse(body, false, model.NameValidationScheme, parser.NewParser(parser.Options{}), slog.Default())
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if groups.Groups[0].Rules[0].For != 0 || time.Duration(groups.Groups[0].Rules[1].For) != 30*time.Second {
		t.Fatal("Operator alert delays changed; a one-minute event must not wait five minutes")
	}
}
