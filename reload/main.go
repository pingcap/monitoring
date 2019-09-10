//go:generate statik -src=./ui
package main

import (
	"fmt"
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
	"os"
	"path/filepath"
)

var (
	watchDir        string
	promUrl         string
	rootStorePath 	string
	subStorePath	string
)

func main() {
	var rootCmd = &cobra.Command{
		Use: "reload",
		Run: func(co *cobra.Command, args []string) {
			startServer()
		},
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			UnknownFlags: true,
		},
	}

	rootCmd.Flags().StringVar(&watchDir,"watch-path", "", "the directory to watch for updates.")
	rootCmd.Flags().StringVar(&rootStorePath,"root-store-path", "/data", "the directory to store updated files.")
	rootCmd.Flags().StringVar(&subStorePath,"sub-store-path", "", "the sub directory to store updated files.")
	rootCmd.Flags().StringVar(&promUrl,"prometheus-url", "http://localhost:9090", "the url to send a request to when the files are updated.")
	rootCmd.MarkFlagRequired("watch-path")

	rootCmd.Execute()
}

func startServer() {
	u, err := url.Parse(promUrl)
	if err != nil {
		panic(errors.Wrap(err, "invalid url"))
	}

	storePath := fmt.Sprintf("%s%clatest-rules%c%s", rootStorePath, filepath.Separator, filepath.Separator, extract(subStorePath))
	if len(subStorePath) != 0 && !exist(storePath) {
		log.Println("need to store latest file to store path")
		if err := os.MkdirAll(storePath, os.ModePerm); err != nil {
			log.Println("failed to create store path", err)
		}
	}

	engine := apis.NewService(u, watchDir, len(subStorePath) != 0, storePath)
	statikFS, err := fs.New()
	if err != nil {
		log.Fatal("failed to embed static files into your binary", err)
	}

	staticHandler := http.FileServer(statikFS)
	engine.NoRoute(func(c *gin.Context) {
		staticHandler.ServeHTTP(c.Writer, c.Request)
	})

	if err := bizlogic.NewReloader(u, watchDir).Start(); err != nil {
		log.Fatal("Watch dir failed", err)
	}

	log.Fatal("StartServer server failed", engine.Run("0.0.0.0:9089").Error())
}

func extract(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == filepath.Separator {
			return path[i + 1:]
		}
	}
	return path
}

func exist(filepath string) bool{
	if _, err := os.Stat(filepath); err != nil {
		return false
	}

	return true
}