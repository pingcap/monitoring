package operator

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/pingcap/monitoring/pkg/common"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
	"gopkg.in/yaml.v3"
)

const forConfig = model.Duration(5 * time.Minute)

func WriteRule(body string, ruleName string, baseDir string, needToReplaceExpr map[string]string) error {
	newRule, err := TransformRules([]byte(body), needToReplaceExpr)
	if err != nil {
		return err
	}

	common.WriteFile(baseDir, ruleName, string(newRule))
	return nil
}

// TransformRules preserves rule-group settings while applying alert overrides.
func TransformRules(content []byte, needToReplaceExpr map[string]string) ([]byte, error) {
	groups, errs := rulefmt.Parse(content, false, model.NameValidationScheme, parser.NewParser(parser.Options{}), slog.Default())
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	for i := range groups.Groups {
		group := &groups.Groups[i]
		for j := range group.Rules {
			rule := &group.Rules[j]
			if rule.Alert == "" {
				continue
			}
			if time.Duration(rule.For) <= time.Minute {
				rule.For = forConfig
			}
			if newExpr, ok := needToReplaceExpr[strings.ToUpper(rule.Alert)]; ok {
				rule.Expr = newExpr
				if _, ok := rule.Labels["expr"]; ok {
					rule.Labels["expr"] = newExpr
				}
			}
		}
	}
	output, err := yaml.Marshal(groups)
	if err != nil {
		return nil, err
	}
	_, errs = rulefmt.Parse(output, false, model.NameValidationScheme, parser.NewParser(parser.Options{}), slog.Default())
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return output, nil
}
