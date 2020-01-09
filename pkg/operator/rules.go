package operator

import (
	"strings"
	"time"

	"github.com/pingcap/monitoring/pkg/common"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/rulefmt"
	"github.com/wushilin/stream"
	"gopkg.in/yaml.v2"
)

const (
	ALERT_FOR_CONFIG = "5m"
)

var (
	forConfig, err = model.ParseDuration(ALERT_FOR_CONFIG)
)

func WriteRule(body string, ruleName string, baseDir string, needToReplaceExpr map[string]string) error {
	newRule, err := replaceAlertExpr([]byte(body), needToReplaceExpr)
	if err != nil {
		return err
	}

	common.WriteFile(baseDir, ruleName, string(newRule))
	return nil
}

func replaceAlertExpr(content []byte, needToReplaceExpr map[string]string) ([]byte, error) {
	var groups rulefmt.RuleGroups
	if err := yaml.UnmarshalStrict(content, &groups); err != nil {
		return nil, err
	}

	var newGS rulefmt.RuleGroups
	for _, group := range groups.Groups {
		newG := rulefmt.RuleGroup{
			Interval: group.Interval,
			Name:     group.Name,
			Rules:    make([]rulefmt.Rule, len(group.Rules)),
		}

		stream.FromArray(group.Rules).Map(func(rule rulefmt.Rule) rulefmt.Rule {
			if time.Duration(rule.For) <= (time.Second * 60) {
				if err != nil {
					rule.For = forConfig
				}
			}

			newExpr, ok := needToReplaceExpr[strings.ToUpper(rule.Alert)]
			if !ok {
				return rule
			}

			rule.Expr = newExpr
			if _, ok := rule.Labels["expr"]; ok {
				rule.Labels["expr"] = newExpr
			}

			return rule
		}).CollectTo(newG.Rules)

		newGS.Groups = append(newGS.Groups, newG)
	}

	return yaml.Marshal(newGS)
}
