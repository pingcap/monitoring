package apis

import (
	"github.com/gin-gonic/gin"
	"github.com/pingcap/monitoring/reload/server/auth"
	"github.com/pingcap/monitoring/reload/server/bizlogic"
	"log/slog"
	"net/http"
	"net/url"
)

func NewService(promUrl *url.URL, watchDir string, needStoreFileToStorePath bool, storePath string, reloaders ...*bizlogic.Reloader) *gin.Engine {
	return NewSecuredService(promUrl, watchDir, needStoreFileToStorePath, storePath, nil, reloaders...)
}

func NewSecuredService(promUrl *url.URL, watchDir string, persist bool, storePath string, policy *auth.Policy, reloaders ...*bizlogic.Reloader) *gin.Engine {
	s := bizlogic.NewServer(promUrl, watchDir, persist, storePath, reloaders...)
	r := gin.New()
	r.Use(gin.Recovery(), auth.Middleware(policy))

	ws := r.Group("monitoring")
	ws.Use(func(c *gin.Context) {
		c.Next()
		args := []any{"event", "rule_api", "actor", c.GetString("actor"), "role", c.GetString("role"), "method", c.Request.Method, "target", c.Param("config"), "status", c.Writer.Status(), "remote", c.Request.RemoteAddr}
		for _, key := range []string{"config_sha256", "config_bytes", "config_saved"} {
			if value, ok := c.Get(key); ok {
				args = append(args, key, value)
			}
		}
		slog.Info("reloader rule operation", args...)
	})
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
