//go:generate statik -src=./ui
package main

import (
	"github.com/gin-gonic/gin"
	"github.com/pingcap/monitoring/reload/server/apis"
	"github.com/pingcap/monitoring/reload/server/bizlogic"
	_ "github.com/pingcap/monitoring/reload/statik"
	"github.com/pkg/errors"
	"github.com/rakyll/statik/fs"
	"github.com/spf13/cobra"
	"log"
	"net/http"
	"net/url"
)

var (
	watchDir        string
	promUrl         string
)

func main() {
	var rootCmd = &cobra.Command{
		Use: "reload",
		Run: func(co *cobra.Command, args []string) {
			startServer()
		},
	}

	rootCmd.Flags().StringVar(&watchDir,"watch-path", "", "the directory to watch for updates.")
	rootCmd.Flags().StringVar(&promUrl,"prometheus-url", "http://localhost:9090", "the url to send a request to when the files are updated.")
	rootCmd.MarkFlagRequired("watch-path")
	rootCmd.Execute()
}

func startServer() {
	u, err := url.Parse(promUrl)
	if err != nil {
		panic(errors.Wrap(err, "invalid url"))
	}

	engine := apis.NewService(u, watchDir)
	statikFS, err := fs.New()
	if err != nil {
		log.Fatal("failed to embed static files into your binary", err)
	}

	staticHandler := http.FileServer(statikFS)
	engine.NoRoute(func(c *gin.Context) {
		staticHandler.ServeHTTP(c.Writer, c.Request)
	})

	if err := bizlogic.NewReloader(u, watchDir).Start(); err != nil {
		log.Fatal("Watch dir failed", )
	}

	log.Fatal("StartServer server failed", engine.Run("0.0.0.0:9089").Error())
}