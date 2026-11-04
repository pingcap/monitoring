package bizlogic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

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

const prometheusRequestTimeout = 10 * time.Second
const maxRulesBytes = 16 << 20

type server struct {
	reload                   func(context.Context) error
	httpClient               *http.Client
	url                      *url.URL
	dir                      string
	storePath                string
	needStoreFileToStorePath bool
}

func NewServer(promURL *url.URL, watchDir string, needStoreFileToStorePath bool, storePath string, reloaders ...*Reloader) *server {
	reloader := NewReloader(promURL, watchDir)
	if len(reloaders) > 0 && reloaders[0] != nil {
		reloader = reloaders[0]
	}
	return &server{
		reload:                   reloader.Reload,
		httpClient:               &http.Client{Timeout: prometheusRequestTimeout},
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
	r, err := http.NewRequestWithContext(c.Request.Context(), "GET", fmt.Sprintf("%s/api/v1/rules", s.url), nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, utils.NewErrorResponse("cannot query Prometheus rules"))
		return
	}

	resp, err := s.httpClient.Do(r)
	if err != nil {
		c.JSON(http.StatusBadGateway, utils.NewErrorResponse("cannot query Prometheus rules"))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		c.JSON(http.StatusBadGateway, utils.NewErrorResponse("Prometheus rules request failed"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRulesBytes+1))
	if err != nil || len(body) > maxRulesBytes {
		c.JSON(http.StatusBadGateway, utils.NewErrorResponse("cannot read Prometheus rules within size limit"))
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
		return err == nil && target.Mode().IsRegular() && target.Size() <= maxConfigBytes
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
	// The primary must still exist after the upload; only the persistence copy may be created.
	if err := writeRegularConfig(root, name, data, 0); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, utils.NewErrorResponse("config not found"))
		} else {
			c.JSON(http.StatusInternalServerError, utils.NewErrorResponse("cannot write config"))
		}
		return
	}
	auditSavedConfig(c, data)
	if s.needStoreFileToStorePath {
		store, err := os.OpenRoot(s.storePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, utils.NewErrorResponse("cannot open persistence directory"))
			return
		}
		defer store.Close()
		if err := writeRegularConfig(store, name, data, os.O_CREATE); err != nil {
			c.JSON(http.StatusInternalServerError, utils.NewErrorResponse("cannot persist config"))
			return
		}
	}
	if err := s.reload(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadGateway, utils.NewErrorResponse("config saved but Prometheus reload failed"))
		return
	}
	c.JSON(http.StatusOK, config)
}

// Check the opened descriptor before truncating; a FIFO replacement must not block the request.
func writeRegularConfig(root *os.Root, name string, data []byte, createFlag int) error {
	f, err := root.OpenFile(name, os.O_WRONLY|nonblockOpenFlag|createFlag, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config is not a regular file")
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Close()
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
