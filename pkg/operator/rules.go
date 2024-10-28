package operator

import (
	"errors"
	"strings"
	"time"

	"github.com/pingcap/monitoring/pkg/common"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/youthlin/stream"
	streamtypes "github.com/youthlin/stream/types"
	"gopkg.in/yaml.v3"
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
	groups, errs := rulefmt.Parse(content)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	var newGS rulefmt.RuleGroups
	for _, group := range groups.Groups {
		newG := rulefmt.RuleGroup{
			Interval: group.Interval,
			Name:     group.Name,
			Rules:    make([]rulefmt.RuleNode, 0, len(group.Rules)),
		}

		stream.OfSlice(group.Rules).Map(func(t streamtypes.T) streamtypes.R {
			rule := t.(rulefmt.RuleNode)
			if time.Duration(rule.For) <= (time.Second * 60) {
				if err != nil {
					rule.For = forConfig
				}
			}

			newExpr, ok := needToReplaceExpr[strings.ToUpper(rule.Alert.Value)]
			if !ok {
				return rule
			}

			rule.Expr.SetString(newExpr)
			if _, ok := rule.Labels["expr"]; ok {
				rule.Labels["expr"] = newExpr
			}

			return rule
		}).ForEach(func(t streamtypes.T) {
			rule := t.(rulefmt.RuleNode)
			newG.Rules = append(newG.Rules, rule)
		})

		newGS.Groups = append(newGS.Groups, newG)
	}

	return yaml.Marshal(newGS)
}
