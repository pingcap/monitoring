// package is used to automic generate directory of dashboards and alert rules.
// The monitor like this:
// monitor
//      v2.1.8
//          dashboards
//               tidb.json
//               ...
//          rules
//                pd.rule.yml
//                ...
//          Dockerfile
//          init.sh
//       ...
package main

import (
	"fmt"
	"github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/wushilin/stream"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/storage/memory"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// expect_basic_file_size is used to check file number in auto generated directory.
const expect_basic_file_size  = 17

var (
	lowest_version string
	repository_url string
	baseDir string
	datasource_name = "tidb-cluster"
	//dashboards = []string{"binlog.json", "tidb.json", "overview.json", "tikv_details.json", "tikv_summary.json", "tikv_trouble_shooting.json", "pd.json", "tikv_pull.json"}

	dashboards = map[string]string{
		"binlog.json": "Test-Cluster-Binlog",
		"tidb.json": "Test-Cluster-TiDB",
		"overview.json": "Test-Cluster-Overview",
		"tikv_details.json": "Test-Cluster-TiKV-Details",
		"tikv_summary.json": "Test-Cluster-TiKV-Summary",
		"tikv_trouble_shooting.json": "Test-Cluster-TiKV-Trouble-Shooting",
		"pd.json": "Test-Cluster-PD",
		"tikv_pull.json": "Test-Cluster-TiKV",
		"overview_pull.json": "Test-Cluster-Overview",
		"lightning.json": "Test-Cluster-Lightning",
	}

	rules = []string{"tidb.rules.yml", "pd.rules.yml", "tikv-pull.rules.yml", "tikv.rules.yml", "binlog.rules.yml", "lightning.rules.yml"}
	overviewExlcudeItems = []string{"Services Port Status", "System Info"}
	tikvExcludeItems = []string{"IO utilization"}
	//dockerfiles = []string{"Dockerfile", "init.sh"}

	localFiles = map[string]string {
		"datasource/k8s-datasource.json": "datasources",
		"datasource/tidb-cluster-datasource.json": "datasources",
		"dashboards/pods/pods.json": "dashboards",
		"dashboards/nodes/nodes.json": "dashboards",
		"Dockerfile": ".",
		"init.sh": ".",
	}
)

func main() {
	var rootCmd = &cobra.Command{
		Use: "monitoring",
		Run: func(co *cobra.Command, args []string) {
			exportMonitorData()
		},
	}

	rootCmd.Flags().StringVar(&baseDir,"path", ".", "the base directory of the program")
	rootCmd.Flags().StringVar( &lowest_version,"lowest-version", "2.1.8", "the lowest tidb version")
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

	stream.FromArray(refs).Filter(func(ref *plumbing.Reference) bool {
		return ref.Name().IsTag()
	}).Map(func(ref *plumbing.Reference) string{
		return ref.Name().Short()
	}).Filter(func(tag string) bool{
		return compareVersion(tag)
	}).Map(func (tag string) string{
		dir := fmt.Sprintf("%s%c%s", monitorDir, filepath.Separator, tag)
		fmt.Println("tagpath=" + tag)

		fetchDashboard(tag, dir)
		fetchRules(tag, dir)
		return dir
	}).Peek(func(dir string) {
		// copy local files
		stream.FromMapEntries(localFiles).Each(func(entry stream.MapEntry) {
			copyLocalfiles(baseDir, dir, entry.Key.(reflect.Value).String(), entry.Value.(string))
		})
	}).Each(func(dir string) {
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
	checkErr(os.MkdirAll(dir, os.ModePerm), "create dir failed, path=" + dir)

	stream.FromMapEntries(dashboards).Each(func(entry stream.MapEntry) {
		dashboard := entry.Key.(reflect.Value).String()
		body := fetchContent(fmt.Sprintf("%s/%s/scripts/%s", repository_url, tag, dashboard), tag, dashboard)
		writeFile(dir, convertDashboardFileName(dashboard), filterDashboard(body, dashboard, entry.Value.(string)))
	})
}

// convertDashboardFileName convert file name
func convertDashboardFileName(dashboard string) string{
	if strings.HasPrefix(dashboard, "overview") {
		return "overview.json"
	}

	return dashboard
}

// fetchRules fetch rules from the source
func fetchRules(tag string, baseDir string) {
	dir := fmt.Sprintf("%s%crules", baseDir, filepath.Separator)
	checkErr(os.MkdirAll(dir, os.ModePerm), "create dir failed, path=" + dir)

	stream.FromArray(rules).Each(func(rule string) {
		body := fetchContent(fmt.Sprintf("%s/%s/roles/prometheus/files/%s", repository_url, tag, rule), tag, rule)
		writeFile(dir, rule, body)
	})
}

func fetchContent(url string, tag string, fileName string) string  {
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
	checkErr(err, "create file failed, f=" + fn)
	defer f.Close()

	if _, err := f.WriteString(body); err != nil {
		checkErr(err, "write file failed, f=" + fn)
	}
}

func filterDashboard(body string, dashboard string, title string) string{
	newStr := ""
	stream.Of(body).Filter(func(str string) bool {
		return str != ""
	}).Map(func(str string) string{
		if dashboard != "overview.json" {
			return str
		}

		stream.FromArray(overviewExlcudeItems).Each(func (item string) {
			str = deleteOverviewItemFromDashboard(str, item)
		})

		return str
	}).Map(func(str string) string {
		if !strings.Contains(dashboard, "tikv") {
			return str
		}

		stream.FromArray(tikvExcludeItems).Each(func (item string) {
			str = deleteTiKVItemFromDashboard(str, item)
		})

		return str
	}).Map(func(str string) string {
		// replace grafana item
		r := gjson.Get(str, "__requires.0.type")
		if r.Exists() && r.Str == "grafana" {
			newStr, err := sjson.Set(str, "__requires.0.version", "")
			checkErr(err, "update links filed failed")
			return newStr
		}

		return str
	}).Map(func (str string) string {
		// replace links item
		if gjson.Get(str, "links").Exists() {
			newStr, err := sjson.Set(str, "links", []struct{}{})
			checkErr(err, "update links filed failed")
			return newStr
		}

		return str
	}).Map(func (str string) string {
		// replace datasource name
		if gjson.Get(str, "__inputs").Exists() && gjson.Get(str, "__inputs.0.name").Exists() {
			datasource := gjson.Get(str, "__inputs.0.name").Str
			return strings.ReplaceAll(str, fmt.Sprintf("${%s}", datasource), datasource_name)
		}
		return str
	}).Map(func(str string)string {
		// delete input defination
		if gjson.Get(str, "__inputs").Exists() {
			newStr, err := sjson.Delete(str, "__inputs")
		    checkErr(err, "delete path failed")
			return newStr
		}

		return str
	}).Map(func (str string) string {
		// unify the title name
		newStr ,err := sjson.Set(str, "title", title)
		checkErr(err, "replace title failed")
		return newStr
	}).Each(func (str string) {
		newStr = str
	})

	return newStr
}

func deleteOverviewItemFromDashboard(source string, itemName string) string{
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

	for index, _ := range  gjson.Get(source, key).Array() {
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