package bizlogic

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/pingcap/monitoring/reload/server/types"
	"github.com/pingcap/monitoring/reload/server/utils"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/youthlin/stream"
	streamtypes "github.com/youthlin/stream/types"
)

type server struct {
	url                      *url.URL
	dir                      string
	storePath                string
	needStoreFileToStorePath bool
}

func NewServer(promURL *url.URL, watchDir string, needStoreFileToStorePath bool, storePath string) *server {
	return &server{
		url:                      promURL,
		dir:                      watchDir,
		needStoreFileToStorePath: needStoreFileToStorePath,
		storePath:                storePath,
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

func (s *server) getConfigs() ([]string, error) {
	files, err := ioutil.ReadDir(s.dir)
	if err != nil {
		return nil, errors.Wrap(err, "get config list failed")
	}

	r := make([]string, 0)
	stream.OfSlice(files).Filter(func(t streamtypes.T) bool {
		info := t.(os.FileInfo)
		return !info.IsDir() && filepath.Ext(info.Name()) == ".yml"
	}).ForEach(func(t streamtypes.T) {
		info := t.(os.FileInfo)
		r = append(r, info.Name())
	})
	return r, nil
}

func (s *server) UpdateConfig(c *gin.Context) {
	configName := utils.GetHttpParameter(c.Param, "config")

	files, err := ioutil.ReadDir(s.dir)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
	} else {
		config := &types.Config{}
		if err := c.ShouldBindJSON(config); err != nil {
			fmt.Println(err)
			msg := fmt.Sprintf("invalid request body: %v", err)
			c.JSON(http.StatusBadRequest, msg)
			return
		}

		stream.OfSlice(files).Filter(func(t streamtypes.T) bool {
			info := t.(os.FileInfo)
			return info.Name() == configName
		}).Map(func(t streamtypes.T) streamtypes.R {
			return []byte(config.Data)
		}).Filter(func(t streamtypes.T) bool {
			data := t.([]byte)
			if err := parse(data); err != nil {
				c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
				return false
			}
			return true
		}).Peek(func(t streamtypes.T) {
			data := t.([]byte)
			if err := ioutil.WriteFile(fmt.Sprintf("%s%c%s", s.dir, filepath.Separator, configName), data, os.ModePerm); err != nil {
				c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
			} else {
				c.JSON(http.StatusOK, config)
			}
		}).Filter(func(t streamtypes.T) bool {
			if !s.needStoreFileToStorePath {
				log.Printf("do not need to store file to storepath")
			}
			return s.needStoreFileToStorePath
		}).ForEach(func(t streamtypes.T) {
			data := t.([]byte)
			if err := ioutil.WriteFile(fmt.Sprintf("%s%c%s", s.storePath, filepath.Separator, configName), data, os.ModePerm); err != nil {
				log.Printf("write file to store path failed: %v", err)
			}
		})
	}
}

func parse(content []byte) error {
	_, errs := rulefmt.Parse([]byte(content))
	if errs == nil || len(errs) == 0 {
		return nil
	}

	var errStr string
	stream.OfSlice(errs).ForEach(func(t streamtypes.T) {
		err := t.(error)
		errStr += err.Error()
	})

	return errors.New(errStr)
}
