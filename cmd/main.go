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
	"strings"
)

var (
	lowest_version string
	repository_url string
	path string
	datasource_name = "tidb-cluster"
	dashboards = []string{"binlog.json", "tidb.json", "overview.json", "tikv_details.json", "tikv_summary.json", "tikv_trouble_shooting.json", "pd.json", "tikv_pull.json"}
	rules = []string{"tidb.rules.yml", "pd.rules.yml", "tikv-pull.rules.yml", "tikv.rules.yml"}
	dockerfiles = []string{"Dockerfile", "init.sh"}
)

func main() {
	var rootCmd = &cobra.Command{
		Use: "monitoring",
		Run: func(co *cobra.Command, args []string) {
			exportMonitorData()
		},
	}

	rootCmd.Flags().StringVar(&path,"path", ".", "the path of export monitor data")
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
	baseDir, err := filepath.Abs(".")
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
	}).Each(func(dir string) {
		stream.FromArray(dockerfiles).Each(func(file string) {
			copyDockerfiles(baseDir, dir, file)
		})
	})
}

// fetchDashboard fetch dashboards from the source and replace some variables in the file.
func fetchDashboard(tag string, baseDir string) {
	dir := fmt.Sprintf("%s%cdashboards", baseDir, filepath.Separator)
	checkErr(os.MkdirAll(dir, os.ModePerm), "create dir failed, path=" + dir)

	stream.FromArray(dashboards).Each(func(dashboard string) {
		body := fetchContent(fmt.Sprintf("%s/%s/scripts/%s", repository_url, tag, dashboard), tag, dashboard)
		writeFile(dir, dashboard, filterDashboard(body))
	})
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

func filterDashboard(body string) string{
	newStr := ""
	stream.Of(body).Filter(func(str string) bool {
		return str != ""
	}).Map(func(str string) string {
		r := gjson.Get(str, "__requires.0.type")
		if r.Exists() && r.Str == "grafana" {
			newStr, err := sjson.Set(str, "__requires.0.version", "")
			checkErr(err, "update links filed failed")
			return newStr
		}

		return str
	}).Map(func (str string) string {
		if gjson.Get(str, "links").Exists() {
			newStr, err := sjson.Set(str, "links", []struct{}{})
			checkErr(err, "update links filed failed")
			return newStr
		}

		return str
	}).Map(func (str string) string {
		if gjson.Get(str, "__inputs").Exists() && gjson.Get(str, "__inputs.0.name").Exists() {
			datasource := gjson.Get(str, "__inputs.0.name").Str
			return strings.ReplaceAll(str, fmt.Sprintf("${%s}", datasource), datasource_name)
		}
		return str
	}).Map(func(str string)string {
		if gjson.Get(str, "__inputs").Exists() {
			newStr, err := sjson.Delete(str, "__inputs")
		    checkErr(err, "delete path failed")
			return newStr
		}

		return str
	}).Each(func (str string) {
		newStr = str
	})

	return newStr
}

func copyDockerfiles(baseDir string, currentDir string, copyFile string) {
	df, err := ioutil.ReadFile(fmt.Sprintf("%s%ccmd%c%s", baseDir, filepath.Separator, filepath.Separator, copyFile))
	checkErr(err, fmt.Sprintf("read file failed, file=%s", copyFile))
	checkErr(ioutil.WriteFile(fmt.Sprintf("%s%c%s", currentDir, filepath.Separator, copyFile), df, os.ModePerm), "create file failed")
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