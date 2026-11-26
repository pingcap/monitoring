package apis

import (
	"github.com/gin-gonic/gin"
	"github.com/pingcap/monitoring/reload/server/bizlogic"
	"net/http"
	"net/url"
)

func NewService(promUrl *url.URL, watchDir string, needStoreFileToStorePath bool, storePath string, reloaders ...*bizlogic.Reloader) *gin.Engine {
	s := bizlogic.NewServer(promUrl, watchDir, needStoreFileToStorePath, storePath, reloaders...)
	r := gin.Default()

	ws := r.Group("monitoring")
	slots := make(chan struct{}, 4)
	ws.Use(func(c *gin.Context) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			c.Next()
		default:
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many concurrent monitoring requests"})
		}
	})

	ws.GET("/rules", s.ListRules)
	ws.GET("/configs", s.ListConfigs)
	ws.GET("/configs/:config", s.GetConfig)
	ws.PUT("/configs/:config", s.UpdateConfig)

	return r
}
