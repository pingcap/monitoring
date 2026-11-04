package main

import (
	"log/slog"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
)

func TestLegacyTransformKeepsRecordingRulesValid(t *testing.T) {
	output, err := replaceAlertExpr([]byte("groups:\n- name: test\n  limit: 12\n  rules:\n  - record: instance:up\n    expr: sum(up)\n"))
	if err != nil {
		t.Fatal(err)
	}
	groups, errs := rulefmt.Parse(output, false, model.NameValidationScheme, parser.NewParser(parser.Options{}), slog.Default())
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if groups.Groups[0].Limit != 12 || groups.Groups[0].Rules[0].For != 0 {
		t.Fatal("recording rule or group settings changed")
	}
}
