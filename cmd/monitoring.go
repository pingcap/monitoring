package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/google/go-github/github"
	"github.com/pingcap/monitoring/pkg/ansible"
	"github.com/pingcap/monitoring/pkg/common"
	"github.com/pingcap/monitoring/pkg/operator"
	traceErr "github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wushilin/stream"
	"gopkg.in/yaml.v2"
)

const (
	Ansible  = "ansible"
	Opertaor = "operator"

	Ansible_Grfana_Dir = "tidb-monitor"
	Ansible_Rule_Dir   = "tidb-rule"
	Commit_Branch      = "auto-generate-for-%s"
)

var (
	platformMonitoringDir string
	configFile            string
	outputDir             string
	autoPush              bool
	cfg                   *Config
	tag                   string

	baseTagDir         string
	ansibleGrafanaDir  string
	ansibleRuleDir     string
	operatorGrafanaDir string
	operatorRuleDir    string

	useGlobalTag  = true
	operatorFiles = map[string]string{
		"datasource": "datasources",
		"grafana":    "dashboards",
		"Dockerfile": ".",
		"init.sh":    ".",
	}

	ansibleFiles = map[string]string{
		"grafana": Ansible_Grfana_Dir,
		"rule":    Ansible_Rule_Dir,
	}

	operatorReplaceExpr = make(map[string]string)
)

func main() {
	var rootCmd = &cobra.Command{
		Use: "load",
		Run: func(co *cobra.Command, args []string) {
			defer func() {
				if err := recover(); err != nil {
					traceE := traceErr.Wrap(err.(error), "")
					fmt.Printf("%+v", traceE)
					os.RemoveAll(baseTagDir)
				} else {
					fmt.Println("Done.")
				}
			}()
			common.CheckErr(stepUp(), "init env failed")
			common.CheckErr(Start(), "generate monitoring configuration failed")
		},
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			UnknownFlags: true,
		},
	}

	rootCmd.Flags().StringVar(&configFile, "config", "", "the monitoring configuration file.")
	rootCmd.Flags().StringVar(&tag, "tag", "", "the tag of pull monitoring repo.")
	rootCmd.Flags().StringVar(&outputDir, "output-dir", ".", "the base directory of the program")
	rootCmd.Flags().StringVar(&platformMonitoringDir, "platform-monitoring-dir", "platform-config", "the direcotry of platform-config in monitoring repo")
	rootCmd.Flags().BoolVar(&autoPush, "auto-push", false, "auto generate new branch from master and push auto-generate files to the branch")
	rootCmd.MarkFlagRequired("config")

	rootCmd.Execute()
}

func stepUp() error {
	content, err := ioutil.ReadFile(configFile)
	if err != nil {
		return err
	}

	cfg, err = Load(string(content))
	if err != nil {
		return err
	}

	if tag == "" {
		for _, cfg := range cfg.ComponentConfigs {
			if cfg.Ref == "" {
				return errors.New(fmt.Sprintf("empty branch/tag info for %s", cfg.RepoName))
			}
		}

		tag = fmt.Sprintf("tmp_branch_%d", time.Now().Unix())
		useGlobalTag = false
	}

	outputDir = removeLastSlash(outputDir)
	baseTagDir = fmt.Sprintf("%s%cmonitor-snapshot%c%s", outputDir, filepath.Separator, filepath.Separator, tag)
	common.CheckErr(os.RemoveAll(baseTagDir), "delete path filed")
	common.CheckErr(os.MkdirAll(baseTagDir, os.ModePerm), "create dir failed, path="+baseTagDir)

	// ansible directory
	ansibleGrafanaDir = fmt.Sprintf("%s%c%s", getAnsibleDir(baseTagDir), filepath.Separator, Ansible_Grfana_Dir)
	common.CheckErr(os.MkdirAll(ansibleGrafanaDir, os.ModePerm), "create dir failed, path="+ansibleGrafanaDir)
	ansibleRuleDir = fmt.Sprintf("%s%c%s", getAnsibleDir(baseTagDir), filepath.Separator, Ansible_Rule_Dir)
	common.CheckErr(os.MkdirAll(ansibleRuleDir, os.ModePerm), "create dir failed, path="+ansibleRuleDir)

	// operator direcotry
	operatorGrafanaDir = fmt.Sprintf("%s%cdashboards", getOperatorDir(baseTagDir), filepath.Separator)
	common.CheckErr(os.MkdirAll(operatorGrafanaDir, os.ModePerm), "create dir failed, path="+operatorGrafanaDir)
	operatorRuleDir = fmt.Sprintf("%s%crules", getOperatorDir(baseTagDir), filepath.Separator)
	common.CheckErr(os.MkdirAll(operatorRuleDir, os.ModePerm), "create dir failed, path="+operatorRuleDir)

	return nil
}

func Start() error {
	rservice, err := RepoService(cfg)
	if err != nil {
		return err
	}

	stream.FromArray(cfg.OperatorConfig.NeedToReplaceExpr).Each(func(expr ReplaceExpr) {
		operatorReplaceExpr[strings.ToUpper(expr.RuleName)] = expr.NewExpr
	})

	stream.FromArray(cfg.ComponentConfigs).Peek(func(component ComponentConfig) {
		ProcessDashboards(fetchDirectory(rservice, component.Owner, component.RepoName, component.MonitorPath, component.Ref), rservice)
	}).Each(func(component ComponentConfig) {
		ProcessRules(fetchDirectory(rservice, component.Owner, component.RepoName, component.RulesPath, component.Ref), rservice)
	})

	// copy ansible platform config
	stream.FromMapEntries(ansibleFiles).Each(func(entry stream.MapEntry) {
		copyAnsibleLocalFiles(entry.Key.(reflect.Value).String(), entry.Value.(string))
	})

	err = ansible.Compress(fmt.Sprintf("%s%c%s", baseTagDir, filepath.Separator, Ansible), fmt.Sprintf("%s%c%s", baseTagDir, filepath.Separator, "ansible-monitor.tar.gz"))
	common.CheckErr(err, "compress ansible directory failed")
	os.RemoveAll(fmt.Sprintf("%s%c%s", baseTagDir, filepath.Separator, Ansible))

	// copy operator platform config
	stream.FromMapEntries(operatorFiles).Each(func(entry stream.MapEntry) {
		copyOperatorLocalfiles(entry.Key.(reflect.Value).String(), entry.Value.(string))
	})

	if autoPush {
		return PushPullRequest()
	}

	return nil
}

func PushPullRequest() error {
	client := func() *github.Client {
		var tp github.BasicAuthTransport
		if cfg.Token != "" {
			tp = github.BasicAuthTransport{
				Username: strings.TrimSpace(cfg.UserName),
				Password: strings.TrimSpace(cfg.Password),
			}
		}

		if cfg.UserName != "" || cfg.Password == "" {
			tp = github.BasicAuthTransport{
				Username: strings.TrimSpace(cfg.UserName),
				Password: strings.TrimSpace(cfg.Password),
			}
		}

		return github.NewClient(tp.Client())
	}()

	ctx := context.Background()

	commitBrach := fmt.Sprintf(Commit_Branch, tag)
	ref, err := common.GetRef(client, commitBrach, ctx)
	if err != nil {
		return err
	}

	if ref == nil {
		return errors.New("No error where returned but the reference is nil")
	}

	tree, err := common.GetTree(client, ref, baseTagDir, ctx, outputDir)
	if err != nil {
		return err
	}

	if err := common.PushCommit(client, ref, tree, ctx, tag, cfg.UserName, cfg.Email); err != nil {
		return err
	}

	return common.CreatePR(client, commitBrach, ctx, tag)
}

func fetchDirectory(rservice *common.GitRepoService, owner string, repoName string, path string, ref string) []*common.RepositoryContent {
	_, monitorDirectory, err := rservice.GetContents(owner, repoName, path, &common.RepositoryContentGetOptions{
		Ref: func() string {
			if useGlobalTag {
				return tag
			}

			return ref
		}(),
	})

	common.CheckErr(err, "")
	if len(monitorDirectory) == 0 {
		common.CheckErr(errors.New("empty monitoring configurations"), "")
	}

	return monitorDirectory
}

func ProcessDashboards(dashboards []*common.RepositoryContent, service *common.GitRepoService) {
	var name string
	stream.FromArray(dashboards).Map(func(dashboard *common.RepositoryContent) string {
		name = *dashboard.Name
		content, err := service.DownloadContents(dashboard)
		common.CheckErr(err, "")

		if content == nil {
			return ""
		}

		return string(content)
	}).Filter(func(content string) bool {
		return content != ""
	}).Peek(func(content string) {
		// ansible
		common.WriteFile(ansibleGrafanaDir, name, content)
	}).Each(func(content string) {
		// operator
		operator.WriteDashboard(operatorGrafanaDir, content, name)
	})
}

func ProcessRules(rules []*common.RepositoryContent, service *common.GitRepoService) {
	var name string
	stream.FromArray(rules).Map(func(rule *common.RepositoryContent) string {
		name = *rule.Name
		content, err := service.DownloadContents(rule)
		common.CheckErr(err, "")

		if content == nil {
			return ""
		}

		return string(content)
	}).Filter(func(content string) bool {
		return content != ""
	}).Peek(func(content string) {
		// ansible
		common.WriteFile(ansibleRuleDir, name, content)
	}).Each(func(content string) {
		// operatotr
		operator.WriteRule(content, name, operatorRuleDir, operatorReplaceExpr)
	})
}

func getAnsibleDir(baseTagDir string) string {
	return fmt.Sprintf("%s%c%s", baseTagDir, filepath.Separator, Ansible)
}

func getOperatorDir(baseTagDir string) string {
	return fmt.Sprintf("%s%c%s", baseTagDir, filepath.Separator, Opertaor)
}

func RepoService(cfg *Config) (*common.GitRepoService, error) {
	if cfg.Token != "" {
		return common.NewGitRepoServiceWithAuth(common.BasicAuthTransport{
			OTP: cfg.Token,
		})
	}

	if cfg.UserName != "" || cfg.Password == "" {
		return common.NewGitRepoServiceWithAuth(common.BasicAuthTransport{
			Username: cfg.UserName,
			Password: cfg.Password,
		})
	}

	return common.NewGitRepoService()
}

func copyOperatorLocalfiles(sourcePath string, dstPath string) {
	operatorConfig := fmt.Sprintf("%s%c%s", getPlatFormConfigDir(), filepath.Separator, Opertaor)
	files := common.ListAllFiles(fmt.Sprintf("%s%c%s", operatorConfig, filepath.Separator, sourcePath))

	stream.FromArray(files).Each(func(file string) {
		df, err := ioutil.ReadFile(file)
		common.CheckErr(err, fmt.Sprintf("read file failed, file=%s", file))

		dstDir := fmt.Sprintf("%s%c%s%c%s", baseTagDir, filepath.Separator, Opertaor, filepath.Separator, dstPath)
		if !common.PathExist(dstDir) {
			os.MkdirAll(dstDir, os.ModePerm)
		}
		common.CheckErr(ioutil.WriteFile(fmt.Sprintf("%s%c%s", dstDir, filepath.Separator, common.ExtractFromPath(file)), df, os.ModePerm), "create file failed")
	})
}

func copyAnsibleLocalFiles(sourcePath string, dstPath string) {
	ansibleConfig := fmt.Sprintf("%s%c%s", getPlatFormConfigDir(), filepath.Separator, Ansible)

	files := common.ListAllFiles(fmt.Sprintf("%s%c%s", ansibleConfig, filepath.Separator, sourcePath))

	stream.FromArray(files).Each(func(file string) {
		df, err := ioutil.ReadFile(file)
		common.CheckErr(err, fmt.Sprintf("read file failed, file=%s", file))

		dstDir := fmt.Sprintf("%s%c%s%c%s", baseTagDir, filepath.Separator, Ansible, filepath.Separator, dstPath)
		if !common.PathExist(dstDir) {
			os.MkdirAll(dstDir, os.ModePerm)
		}
		common.CheckErr(ioutil.WriteFile(fmt.Sprintf("%s%c%s", dstDir, filepath.Separator, common.ExtractFromPath(file)), df, os.ModePerm), "create file failed")
	})
}

func getPlatFormConfigDir() string {
	return removeLastSlash(platformMonitoringDir)
}

func removeLastSlash(str string) string {
	return strings.TrimRight(str, "/")
}

// Load parses the YAML input s into a Config.
func Load(s string) (*Config, error) {
	cfg := &Config{}

	err := yaml.UnmarshalStrict([]byte(s), cfg)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

type Config struct {
	UserName         string            `yaml:"user_name,omitempty"`
	Password         string            `yaml:"password,omitempty"`
	Email            string            `yaml:"email"`
	Token            string            `yaml:"token,omitempty"`
	ComponentConfigs []ComponentConfig `yaml:"components"`
	OperatorConfig   OperatorConfig    `yaml:"operator"`
}

type ComponentConfig struct {
	RepoName    string `yaml:"repo_name"`
	MonitorPath string `yaml:"monitor_path"`
	RulesPath   string `yaml:"rule_path"`
	Ref         string `yaml:"ref"`
	Owner       string `yaml:"owner,omitempty"`
}

type OperatorConfig struct {
	NeedToReplaceExpr []ReplaceExpr `yaml:"replace_expr"`
}

type ReplaceExpr struct {
	RuleName string `yaml:"rule_name"`
	NewExpr  string `yaml:"expr"`
}
