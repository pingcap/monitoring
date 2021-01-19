package operator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pingcap/monitoring/pkg/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/wushilin/stream"
)

const (
	datasource_name = "tidb-cluster"
)

var (
	overviewExlcudeItems = []string{"Services Port Status", "System Info"}
	tikvExcludeItems     = []string{"IO utilization"}

	dashboards = map[string]string{
		"binlog.json":                "Test-Cluster-Binlog",
		"tidb.json":                  "Test-Cluster-TiDB",
		"overview.json":              "Test-Cluster-Overview",
		"tikv_details.json":          "Test-Cluster-TiKV-Details",
		"tikv_summary.json":          "Test-Cluster-TiKV-Summary",
		"tikv_trouble_shooting.json": "Test-Cluster-TiKV-Trouble-Shooting",
		"pd.json":                    "Test-Cluster-PD",
		"tikv_pull.json":             "Test-Cluster-TiKV",
		"overview_pull.json":         "Test-Cluster-Overview",
		"lightning.json":             "Test-Cluster-Lightning",
		"tiflash_summary.json":       "Test-Cluster-TiFlash-Summary",
		"tiflash_proxy_summary.json": "Test-Cluster-TiFlash-Proxy-Summary",
		"ticdc.json":                 "Test-Cluster-TiCDC",
	}
)

func WriteDashboard(dir string, body string, name string) error {
	title, exist := dashboards[name]
	if !exist {
		return errors.New(fmt.Sprintf("%s dashboard is not found in operator", name))
	}

	common.WriteFile(dir, convertDashboardFileName(name), filterDashboard(body, name, title))
	return nil
}

// convertDashboardFileName convert file name
func convertDashboardFileName(dashboard string) string {
	if strings.HasPrefix(dashboard, "overview") {
		return "overview.json"
	}

	return dashboard
}

func filterDashboard(body string, dashboard string, title string) string {
	newStr := ""
	stream.Of(body).Filter(func(str string) bool {
		return str != ""
	}).Map(func(str string) string {
		if dashboard != "overview.json" {
			return str
		}

		stream.FromArray(overviewExlcudeItems).Each(func(item string) {
			str = deleteOverviewItemFromDashboard(str, item)
		})

		return str
	}).Map(func(str string) string {
		if !strings.Contains(dashboard, "tikv") {
			return str
		}

		stream.FromArray(tikvExcludeItems).Each(func(item string) {
			str = deleteTiKVItemFromDashboard(str, item)
		})

		return str
	}).Map(func(str string) string {
		// replace grafana item
		r := gjson.Get(str, "__requires.0.type")
		if r.Exists() && r.Str == "grafana" {
			newStr, err := sjson.Set(str, "__requires.0.version", "")
			common.CheckErr(err, "update links filed failed")
			return newStr
		}

		return str
	}).Map(func(str string) string {
		// replace links item
		if gjson.Get(str, "links").Exists() {
			newStr, err := sjson.Set(str, "links", []struct{}{})
			common.CheckErr(err, "update links filed failed")
			return newStr
		}

		return str
	}).Map(func(str string) string {
		// replace datasource name
		if gjson.Get(str, "__inputs").Exists() && gjson.Get(str, "__inputs.0.name").Exists() {
			datasource := gjson.Get(str, "__inputs.0.name").Str
			return strings.ReplaceAll(str, fmt.Sprintf("${%s}", datasource), datasource_name)
		}
		return str
	}).Map(func(str string) string {
		// delete input defination
		if gjson.Get(str, "__inputs").Exists() {
			newStr, err := sjson.Delete(str, "__inputs")
			common.CheckErr(err, "delete path failed")
			return newStr
		}

		return str
	}).Map(func(str string) string {
		// unify the title name
		newStr, err := sjson.Set(str, "title", title)
		common.CheckErr(err, "replace title failed")
		return newStr
	}).Each(func(str string) {
		newStr = str
	})

	return newStr
}

func deleteOverviewItemFromDashboard(source string, itemName string) string {
	key := getRowsOrPannels(source)

	for index, r := range gjson.Get(source, key).Array() {
		if r.Map()["title"].Str == itemName {
			return deleteItem(source, fmt.Sprintf("%s.%d", key, index))
		}
	}

	return source
}

func deleteTiKVItemFromDashboard(source string, itemName string) string {
	key := getRowsOrPannels(source)

	for index, _ := range gjson.Get(source, key).Array() {
		for index2, r2 := range gjson.Get(source, fmt.Sprintf("%s.%d.panels", key, index)).Array() {
			if r2.Map()["title"].Str == itemName {
				return deleteItem(source, fmt.Sprintf("%s.%d.panels.%d", key, index, index2))
			}
		}
	}

	return source
}

func getRowsOrPannels(source string) string {
	key := "rows"
	if !gjson.Get(source, "rows").Exists() {
		key = "panels"
	}

	return key
}

func deleteItem(source string, path string) string {
	newStr, err := sjson.Delete(source, path)
	common.CheckErr(err, fmt.Sprintf("delete path failed, path=%s", path))
	return newStr
}
