package apis

import (
	"github.com/gin-gonic/gin"
	"github.com/pingcap/monitoring/reload/server/bizlogic"
	"net/url"
)

func NewService(promUrl *url.URL, watchDir string) *gin.Engine {
	s := bizlogic.NewServer(promUrl, watchDir)
	r := gin.Default()

	ws := r.Group("monitoring")

	ws.GET("/rules", s.ListRules)
	ws.GET("/configs", s.ListConfigs)
	ws.GET("/configs/:config", s.GetConfig)
	ws.PUT("/configs/:config", s.UpdateConfig)

	return r
}