package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/VictoriaMetrics/metricsql"
	"github.com/google/go-cmp/cmp"
	"github.com/grafana-tools/sdk"
)

const ExternalLabel = "External_Label"

var prefix = os.Getenv("GOPATH") + "/src/github.com/pingcap/monitoring/monitor-snapshot/v4.0.9/operator/dashboards/"

var files = []string{
	"overview.json",
	"pd.json",
	"tiflash_summary.json",
	"lightning.json",
	"tikv_details.json",
	"tidb.json",
	"tikv_summary.json",
	"tiflash_proxy_summary.json",
	"tikv_trouble_shooting.json",
}

func main() {
	for _, file := range files {
		// 1. metrics_name => metrics_name{}
		appendParenthesesToMetricsName(prefix + file)
		// 2. metrics_name{} => metrics_name{EXTERNAL_LABEL}
		addExternalLabelToExpr(prefix + file)
		// TODO: 3. update grafana templating
	}
}

func addExternalLabelToExpr(filename string) {
	fi, err := os.Open(filename)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}
	defer fi.Close()

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		fatal(err)
	}
	origin := string(data)

	br := bufio.NewReader(fi)
	for {
		a, _, c := br.ReadLine()
		if c == io.EOF {
			break
		}
		// avoiod repeat add EXTERNAL_LABEL
		if !strings.Contains(string(a), ExternalLabel) {
			if strings.Contains(string(a), `"expr":`) {
				if strings.Contains(string(a), `{}`) {
					new := strings.ReplaceAll(string(a), `{`, `{EXTERNAL_LABEL`)
					origin = strings.ReplaceAll(origin, string(a), new)
				} else {
					if strings.Contains(string(a), `{`) {
						new := strings.ReplaceAll(string(a), `{`, `{EXTERNAL_LABEL, `)
						origin = strings.ReplaceAll(origin, string(a), new)
					}
				}
			}
		}
	}
	err = ioutil.WriteFile(filename, []byte(origin), 0644)
	if err != nil {
		fatal(err)
	}
}

func appendParenthesesToMetricsName(filename string) {
	var board sdk.Board
	raw, err := ioutil.ReadFile(filename)
	if err != nil {
		fatal(err)
	}
	err = json.Unmarshal(raw, &board)
	if err != nil {
		fatal(fmt.Errorf("failed unmarshal,filename: %s,error: %v", filename, err))
	}

	var panels []sdk.Panel
	for _, panel := range board.Panels {
		panels = append(panels, *panel)
	}

	// get all targets from grafana dashboard panels
	targets := ExtractTargetsFromPanels(panels)

	replaceItems := make(map[string]string)
	// if expr do not have {}, we will append {} to metrics_name
	for _, target := range targets {

		if !strings.Contains(target.Expr, `{`) {
			fmt.Println(target.Expr)
			// get all metrics name from expr
			metricsNames, err := getAllMetricNamesFromExpr(target.Expr)
			if err != nil {
				fatal(err)
			}
			if len(metricsNames) == 0 {
				fatal(fmt.Errorf("no metrics name in expr???"))
			}
			for _, name := range metricsNames {
				replaceItems[name] = name + "{}"
			}
		}
	}
	// "pd_regions_status{}{instance=\"$instance\"}" => "pd_regions_status{instance=\"$instance\"}"
	replaceItems["{}{"] = "{"

	old := string(raw)
	new := string(raw)
	for k, v := range replaceItems {
		new = strings.ReplaceAll(new, k, v)
	}
	diff := cmp.Diff(old, new)
	fmt.Println(diff)
	err = ioutil.WriteFile(filename, []byte(new), 0644)
	if err != nil {
		fatal(err)
	}
}

func ExtractTargetsFromPanels(panels []sdk.Panel) []sdk.Target {
	var result []sdk.Target
	if len(panels) == 0 {
		return result
	}
	for _, panel := range panels {
		targets := panel.GetTargets()
		if targets != nil {
			for _, target := range *targets {
				result = append(result, target)
			}
		}

		if panel.RowPanel != nil {
			result = append(result, ExtractTargetsFromPanels(panel.RowPanel.Panels)...)
		}
	}
	return result
}

// remove all modifier from expr, just return metrics name
func getAllMetricNamesFromExpr(exprStr string) ([]string, error) {
	var result []string
	expr, err := metricsql.Parse(exprStr)
	if err != nil {
		return result, err
	}

	f := func(e metricsql.Expr) {
		switch expr := e.(type) {
		case *metricsql.BinaryOpExpr:
		case *metricsql.FuncExpr:
		case *metricsql.AggrFuncExpr:
		case *metricsql.RollupExpr:
		default:
			exprStr := string(expr.AppendString(nil))
			_, err := strconv.ParseFloat(exprStr, 2)
			if err != nil {
				result = append(result, exprStr)
			}
		}
	}

	VisitAll(expr, f)
	return result, nil
}

// visitAll recursively calls f for all the Expr children in e.
// It visits leaf children at first and then visits parent nodes.
func VisitAll(e metricsql.Expr, f func(expr metricsql.Expr)) {
	switch expr := e.(type) {
	case *metricsql.BinaryOpExpr:
		VisitAll(expr.Left, f)
		VisitAll(expr.Right, f)
	case *metricsql.FuncExpr:
		for _, arg := range expr.Args {
			VisitAll(arg, f)
		}
	case *metricsql.AggrFuncExpr:
		for _, arg := range expr.Args {
			VisitAll(arg, f)
		}
	case *metricsql.RollupExpr:
		VisitAll(expr.Expr, f)
	}
	f(e)
}

func fatal(err error) {
	fmt.Println(err)
	os.Exit(1)
}
