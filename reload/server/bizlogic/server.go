package bizlogic

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/pingcap/monitoring/reload/server/types"
	"github.com/pingcap/monitoring/reload/server/utils"
	"github.com/pkg/errors"
	"github.com/prometheus/common/log"
	"github.com/prometheus/prometheus/pkg/rulefmt"
	"github.com/wushilin/stream"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

type server struct {
	url *url.URL
	dir string
	storePath string
	needStoreFileToStorePath bool
}

func NewServer(promURL *url.URL, watchDir string, needStoreFileToStorePath bool, storePath string) *server {
	return &server{
		url: promURL,
		dir: watchDir,
		needStoreFileToStorePath: needStoreFileToStorePath,
		storePath: storePath,
	}
}

func (s *server) ListConfigs(c *gin.Context) {
	list, err := s.getConfigs()
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
	} else {
		c.JSON(http.StatusOK, list)
	}
}

func (s *server) GetConfig(c *gin.Context) {
	configName := utils.GetHttpParameter(c.Param, "config")
	v, err := ioutil.ReadFile(fmt.Sprintf("%s%c%s", s.dir, filepath.Separator, configName))
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
	} else {
		c.JSON(http.StatusOK, string(v))
	}
}

func (s *server) ListRules(c *gin.Context) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/rules", s.url), nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
		return
	}

	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		c.JSON(http.StatusBadRequest, errors.New(fmt.Sprintf("request failed, code=%d", resp.StatusCode)))
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
	} else {
		c.Header("Content-Type", "application/json")
		c.Writer.Write(body)
	}
}

func (s *server) getConfigs()([]string, error) {
	files, err := ioutil.ReadDir(s.dir)
	if err != nil {
		return nil, errors.Wrap(err, "get config list failed")
	}

	r := make([]string, 0)
	stream.FromArray(files).Filter(func(info os.FileInfo) bool {
		return !info.IsDir() && filepath.Ext(info.Name()) == ".yml"
	}).Each(func(info os.FileInfo) {
		r = append(r, info.Name())
	})
	return r, nil
}

func (s *server) UpdateConfig(c *gin.Context) {
	configName := utils.GetHttpParameter(c.Param, "config")

	files, err := ioutil.ReadDir(s.dir)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
	}  else {
		config := &types.Config{}
		if err := c.ShouldBindJSON(config); err != nil {
			fmt.Println(err)
			msg := fmt.Sprintf("invalid request body: %v", err)
			c.JSON(http.StatusBadRequest, msg)
			return
		}

		stream.FromArray(files).Filter(func(info os.FileInfo) bool {
			return info.Name() == configName
		}).Map(func(info os.FileInfo) []byte {
			return []byte(config.Data)
		}).Filter(func (data []byte) bool {
			if err := parse(data); err != nil {
				c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
				return false
			}
			return true
		}).Peek(func (data []byte) {
			if err := ioutil.WriteFile(fmt.Sprintf("%s%c%s", s.dir, filepath.Separator, configName), data, os.ModePerm); err != nil {
				c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
			} else {
				c.JSON(http.StatusOK, config)
			}
		}).Filter(func (data []byte) bool{
			if !s.needStoreFileToStorePath {
				log.Info("do not need to store file to storepath")
			}
			return s.needStoreFileToStorePath
		}).Each(func (data []byte) {
			if err := ioutil.WriteFile(fmt.Sprintf("%s%c%s", s.storePath, filepath.Separator, configName), data, os.ModePerm); err != nil {
				log.Error("write file to store path failed", err)
			}
		})
	}
}

func parse(content []byte) error {
	var groups rulefmt.RuleGroups
	if err := yaml.UnmarshalStrict(content, &groups); err != nil {
		return err
	}

	errs := groups.Validate()
	if errs == nil || len(errs) == 0 {
		return nil
	}

	var errStr string
	stream.FromArray(errs).Each(func (err error) {
		errStr += err.Error()
	})

	return errors.New(errStr)
}
