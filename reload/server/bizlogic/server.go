package bizlogic

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/pingcap/monitoring/reload/server/types"
	"github.com/pingcap/monitoring/reload/server/utils"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
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

const maxConfigBytes = 8 << 20

func validConfigName(name string) bool {
	return name != "" && name == filepath.Base(name) && filepath.Ext(name) == ".yml"
}

func (s *server) GetConfig(c *gin.Context) {
	name := utils.GetHttpParameter(c.Param, "config")
	if !validConfigName(name) {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse("invalid config name"))
		return
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse("cannot open config directory"))
		return
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_RDONLY|nonblockOpenFlag, 0)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse("cannot open config"))
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse("invalid config file"))
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil || len(data) > maxConfigBytes {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse("cannot read config within size limit"))
		return
	}
	c.JSON(http.StatusOK, string(data))
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
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return nil, errors.Wrap(err, "get config list failed")
	}
	defer root.Close()

	r := make([]string, 0)
	stream.OfSlice(files).Filter(func(t streamtypes.T) bool {
		info := t.(os.FileInfo)
		if info.IsDir() || filepath.Ext(info.Name()) != ".yml" {
			return false
		}
		target, err := root.Stat(info.Name())
		return err == nil && target.Mode().IsRegular()
	}).ForEach(func(t streamtypes.T) {
		info := t.(os.FileInfo)
		r = append(r, info.Name())
	})
	return r, nil
}

func (s *server) UpdateConfig(c *gin.Context) {
	name := utils.GetHttpParameter(c.Param, "config")
	if !validConfigName(name) {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse("invalid config name"))
		return
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse("cannot open config directory"))
		return
	}
	defer root.Close()
	info, err := root.Stat(name)
	if err != nil || !info.Mode().IsRegular() {
		c.JSON(http.StatusNotFound, utils.NewErrorResponse("config not found"))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxConfigBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse("invalid or oversized request body"))
		return
	}
	config := &types.Config{}
	if err := json.Unmarshal(body, config); err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse("invalid or oversized request body"))
		return
	}
	data := []byte(config.Data)
	if err := parse(data); err != nil {
		c.JSON(http.StatusBadRequest, utils.NewErrorResponse(err.Error()))
		return
	}
	// Open without O_CREATE or O_TRUNC: a deleted/replaced file must not be recreated or truncated before checking its type.
	f, err := root.OpenFile(name, os.O_WRONLY|nonblockOpenFlag, 0)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, utils.NewErrorResponse("config not found"))
		} else {
			c.JSON(http.StatusInternalServerError, utils.NewErrorResponse("cannot open config"))
		}
		return
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		c.JSON(http.StatusNotFound, utils.NewErrorResponse("config not found"))
		return
	}
	if err := f.Truncate(0); err != nil {
		c.JSON(http.StatusInternalServerError, utils.NewErrorResponse("cannot write config"))
		return
	}
	if _, err := f.Write(data); err != nil {
		c.JSON(http.StatusInternalServerError, utils.NewErrorResponse("cannot write config"))
		return
	}
	if err := f.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, utils.NewErrorResponse("cannot close config"))
		return
	}
	if s.needStoreFileToStorePath {
		store, err := os.OpenRoot(s.storePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, utils.NewErrorResponse("cannot open persistence directory"))
			return
		}
		defer store.Close()
		if err := store.WriteFile(name, data, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, utils.NewErrorResponse("cannot persist config"))
			return
		}
	}
	c.JSON(http.StatusOK, config)
}

func parse(content []byte) error {
	_, errs := rulefmt.Parse([]byte(content), false, model.NameValidationScheme, parser.NewParser(parser.Options{}), slog.Default())
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
