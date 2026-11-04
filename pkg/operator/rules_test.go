package operator

import (
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
)

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
	output, err := TransformRules(input, map[string]string{"SHORTALERT": "up < 1"})
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
	output, err := TransformRules([]byte("groups:\n- name: test\n  rules:\n  - alert: TestAlert\n    expr: up == 0\n"), map[string]string{"TESTALERT": "up{"})
	if err == nil || output != nil {
		t.Fatalf("invalid replacement accepted: %s, %v", output, err)
	}
}
