// package is used to automic generate directory of dashboards and alert rules.
// The monitor like this:
// monitor
//
//	v2.1.8
//	    dashboards
//	         tidb.json
//	         ...
//	    rules
//	          pd.rule.yml
//	          ...
//	    Dockerfile
//	    init.sh
//	 ...
package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/spf13/cobra"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/youthlin/stream"
	streamtypes "github.com/youthlin/stream/types"
	"gopkg.in/yaml.v2"
)

const (
	// expect_basic_file_size is used to check file number in auto generated directory.
	expect_basic_file_size = 17
	ALERT_FOR_CONFIG       = "5m"
)

var (
	lowest_version  string
	repository_url  string
	baseDir         string
	datasource_name = "tidb-cluster"
	//dashboards = []string{"binlog.json", "tidb.json", "overview.json", "tikv_details.json", "tikv_summary.json", "tikv_trouble_shooting.json", "pd.json", "tikv_pull.json"}

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
	}

	rules                = []string{"tidb.rules.yml", "pd.rules.yml", "tikv-pull.rules.yml", "tikv.rules.yml", "binlog.rules.yml", "lightning.rules.yml", "tiflash.rules.yml"}
	overviewExlcudeItems = []string{"Services Port Status", "System Info"}
	tikvExcludeItems     = []string{"IO utilization"}
	//dockerfiles = []string{"Dockerfile", "init.sh"}

	localFiles = map[string]string{
		"datasource/k8s-datasource.yaml":          "datasources",
		"datasource/tidb-cluster-datasource.yaml": "datasources",
		"dashboards/pods/pods.json":               "dashboards",
		"dashboards/nodes/nodes.json":             "dashboards",
		"Dockerfile":                              ".",
		"init.sh":                                 ".",
	}

	needToReplaceExpr = map[string]string{
		strings.ToUpper("pd_cluster_low_space"):              `(sum(pd_cluster_status{type="store_low_space_count"}) by (instance) > 0) and (sum(etcd_server_is_leader) by (instance) > 0)`,
		strings.ToUpper("pd_cluster_lost_connect_tikv_nums"): `(sum ( pd_cluster_status{type="store_disconnected_count"} ) by (instance) > 0) and (sum(etcd_server_is_leader) by (instance) > 0)`,
		strings.ToUpper("pd_pending_peer_region_count"):      `(sum( pd_regions_status{type="pending_peer_region_count"} ) by (instance)  > 100) and (sum(etcd_server_is_leader) by (instance) > 0)`,
	}

	forConfig, configerr = model.ParseDuration(ALERT_FOR_CONFIG)
)

func main() {
	checkErr(configerr, "config for duration failed")
	var rootCmd = &cobra.Command{
		Use: "monitoring",
		Run: func(co *cobra.Command, args []string) {
			exportMonitorData()
		},
	}

	rootCmd.Flags().StringVar(&baseDir, "path", ".", "the base directory of the program")
	rootCmd.Flags().StringVar(&lowest_version, "lowest-version", "2.1.8", "the lowest tidb version")
	rootCmd.Flags().StringVar(&repository_url, "source-url", "https://raw.githubusercontent.com/pingcap/tidb-ansible", "the tidb monitor source address")
	rootCmd.MarkFlagRequired("path")
	rootCmd.Execute()
}

func exportMonitorData() {
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{"https://github.com/pingcap/tidb-ansible"},
	})

	refs, err := rem.List(&git.ListOptions{})
	checkErr(err, "list reference failed")

	// baseDir is $path/tidb-dashboards
	checkErr(err, "check baseDir failed")
	monitorDir := fmt.Sprintf("%s%cmonitor", baseDir, filepath.Separator)
	checkErr(os.RemoveAll(monitorDir), "delete path filed")

	stream.OfSlice(refs).Filter(func(t streamtypes.T) bool {
		ref := t.(*plumbing.Reference)
		return ref.Name().IsTag()
	}).Map(func(t streamtypes.T) streamtypes.R {
		ref := t.(*plumbing.Reference)
		return ref.Name().Short()
	}).Filter(func(t streamtypes.T) bool {
		tag := t.(string)
		return compareVersion(tag)
	}).Map(func(t streamtypes.T) streamtypes.R {
		tag := t.(string)
		dir := fmt.Sprintf("%s%c%s", monitorDir, filepath.Separator, tag)
		fmt.Println("tagpath=" + tag)

		fetchDashboard(tag, dir)
		fetchRules(tag, dir)
		return dir
	}).Peek(func(t streamtypes.T) {
		dir := t.(string)
		// copy local files
		stream.OfMap(localFiles).ForEach(func(t streamtypes.T) {
			pair := t.(streamtypes.Pair)
			key, val := pair.First.(string), pair.Second.(string)
			copyLocalfiles(baseDir, dir, key, val)
		})
	}).ForEach(func(t streamtypes.T) {
		dir := t.(string)
		// check dir files
		count := 0
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if !info.IsDir() {
				count++
			}
			return nil
		})

		if count < expect_basic_file_size {
			checkErr(errors.New("file number is not matched"), fmt.Sprintf("dir=%s, expectSize=%d, actualSize=%d", dir, expect_basic_file_size, count))
		}
	})
}

// fetchDashboard fetch dashboards from the source and replace some variables in the file.
func fetchDashboard(tag string, baseDir string) {
	dir := fmt.Sprintf("%s%cdashboards", baseDir, filepath.Separator)
	checkErr(os.MkdirAll(dir, os.ModePerm), "create dir failed, path="+dir)

	stream.OfMap(dashboards).ForEach(func(t streamtypes.T) {
		pair := t.(streamtypes.Pair)
		key, val := pair.First.(string), pair.Second.(string)

		dashboard := key
		body := fetchContent(fmt.Sprintf("%s/%s/scripts/%s", repository_url, tag, dashboard), tag, dashboard)
		writeFile(dir, convertDashboardFileName(dashboard), filterDashboard(body, dashboard, val))
	})
}

// convertDashboardFileName convert file name
func convertDashboardFileName(dashboard string) string {
	if strings.HasPrefix(dashboard, "overview") {
		return "overview.json"
	}

	return dashboard
}

// fetchRules fetch rules from the source
func fetchRules(tag string, baseDir string) {
	dir := fmt.Sprintf("%s%crules", baseDir, filepath.Separator)
	checkErr(os.MkdirAll(dir, os.ModePerm), "create dir failed, path="+dir)

	stream.OfSlice(rules).ForEach(func(t streamtypes.T) {
		rule := t.(string)
		body := fetchContent(fmt.Sprintf("%s/%s/roles/prometheus/files/%s", repository_url, tag, rule), tag, rule)
		if body == "" {
			return
		}

		newRule, err := replaceAlertExpr([]byte(body))
		checkErr(err, "replace expr failed")

		writeFile(dir, rule, string(newRule))
	})
}

func fetchContent(url string, tag string, fileName string) string {
	r, err := http.NewRequest("GET", url, nil)
	checkErr(err, "request body failed")

	c := &http.Client{}
	res, err := c.Do(r)
	checkErr(err, "fetch content failed")
	defer res.Body.Close()

	if res.StatusCode == 404 {
		return ""
	}

	if res.StatusCode != 200 {
		checkErr(errors.New(fmt.Sprintf("fetch content failed, tag=%s, file=%s", tag, fileName)), "")
	}

	body, err := ioutil.ReadAll(res.Body)
	checkErr(err, fmt.Sprintf("read content failed, tag=%s, file=%s", tag, fileName))

	return string(body)
}

func writeFile(baseDir string, fileName string, body string) {
	if body == "" {
		return
	}

	fn := fmt.Sprintf("%s%c%s", baseDir, filepath.Separator, fileName)
	f, err := os.Create(fn)
	checkErr(err, "create file failed, f="+fn)
	defer f.Close()

	if _, err := f.WriteString(body); err != nil {
		checkErr(err, "write file failed, f="+fn)
	}
}

func filterDashboard(body string, dashboard string, title string) string {
	newStr := ""
	stream.Of(body).Filter(func(t streamtypes.T) bool {
		str := t.(string)

		return str != ""
	}).Map(func(t streamtypes.T) streamtypes.R {
		str := t.(string)

		if dashboard != "overview.json" {
			return str
		}

		stream.OfSlice(overviewExlcudeItems).ForEach(func(t streamtypes.T) {
			item := t.(string)
			str = deleteOverviewItemFromDashboard(str, item)
		})

		return str
	}).Map(func(t streamtypes.T) streamtypes.R {
		str := t.(string)

		if !strings.Contains(dashboard, "tikv") {
			return str
		}

		stream.OfSlice(tikvExcludeItems).ForEach(func(t streamtypes.T) {
			item := t.(string)

			str = deleteTiKVItemFromDashboard(str, item)
		})

		return str
	}).Map(func(t streamtypes.T) streamtypes.R {
		str := t.(string)

		// replace grafana item
		r := gjson.Get(str, "__requires.0.type")
		if r.Exists() && r.Str == "grafana" {
			newStr, err := sjson.Set(str, "__requires.0.version", "")
			checkErr(err, "update links filed failed")
			return newStr
		}

		return str
	}).Map(func(t streamtypes.T) streamtypes.R {
		str := t.(string)

		// replace links item
		if gjson.Get(str, "links").Exists() {
			newStr, err := sjson.Set(str, "links", []struct{}{})
			checkErr(err, "update links filed failed")
			return newStr
		}

		return str
	}).Map(func(t streamtypes.T) streamtypes.R {
		str := t.(string)

		// replace datasource name
		if gjson.Get(str, "__inputs").Exists() && gjson.Get(str, "__inputs.0.name").Exists() {
			datasource := gjson.Get(str, "__inputs.0.name").Str
			return strings.ReplaceAll(str, fmt.Sprintf("${%s}", datasource), datasource_name)
		}
		return str
	}).Map(func(t streamtypes.T) streamtypes.R {
		str := t.(string)

		// delete input defination
		if gjson.Get(str, "__inputs").Exists() {
			newStr, err := sjson.Delete(str, "__inputs")
			checkErr(err, "delete path failed")
			return newStr
		}

		return str
	}).Map(func(t streamtypes.T) streamtypes.R {
		str := t.(string)

		// unify the title name
		newStr, err := sjson.Set(str, "title", title)
		checkErr(err, "replace title failed")
		return newStr
	}).ForEach(func(t streamtypes.T) {
		str := t.(string)

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
	checkErr(err, fmt.Sprintf("delete path failed, path=%s", path))
	return newStr
}

func copyLocalfiles(baseDir string, currentDir string, sourceFile string, dstPath string) {
	df, err := ioutil.ReadFile(fmt.Sprintf("%s%ccmd%c%s", baseDir, filepath.Separator, filepath.Separator, sourceFile))
	checkErr(err, fmt.Sprintf("read file failed, file=%s", sourceFile))
	dstDir := fmt.Sprintf("%s%c%s", currentDir, filepath.Separator, dstPath)
	if !exist(dstDir) {
		os.Mkdir(dstDir, os.ModePerm)
	}
	checkErr(ioutil.WriteFile(fmt.Sprintf("%s%c%s", dstDir, filepath.Separator, extract(sourceFile)), df, os.ModePerm), "create file failed")
}

func checkErr(err error, msg string) {
	if err != nil {
		panic(errors.Wrap(err, msg))
	}
}

func compareVersion(tag string) bool {
	v1, err := version.NewVersion(lowest_version)
	checkErr(err, "")
	v2, err := version.NewVersion(tag)
	checkErr(err, "")

	return v2.GreaterThanOrEqual(v1)
}

func extract(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == filepath.Separator {
			return path[i:]
		}
	}
	return path
}

func exist(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	} else {
		return true
	}
}

func replaceAlertExpr(content []byte) ([]byte, error) {
	var groups rulefmt.RuleGroups
	if err := yaml.UnmarshalStrict(content, &groups); err != nil {
		return nil, err
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
				rule.For = forConfig
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
