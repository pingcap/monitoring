//go:generate statik -src=./ui
package main

import (
	"crypto/tls"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/pingcap/monitoring/reload/identity"
	"github.com/pingcap/monitoring/reload/server/apis"
	"github.com/pingcap/monitoring/reload/server/auth"
	"github.com/pingcap/monitoring/reload/server/bizlogic"
	_ "github.com/pingcap/monitoring/reload/statik"
	"github.com/pkg/errors"
	"github.com/rakyll/statik/fs"
	"github.com/spf13/cobra"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

var (
	listenAddress string
	authFile      string
	tlsCertFile   string
	tlsKeyFile    string
	allowInsecure bool
	watchDir      string
	promUrl       string
	rootStorePath string
	subStorePath  string
)

func main() {
	var rootCmd = &cobra.Command{
		Use: "reload",
		Run: func(co *cobra.Command, args []string) {
			if subStorePath != "" && (extract(subStorePath) == "" || extract(subStorePath) == "." || extract(subStorePath) == "..") {
				log.Fatal("invalid persistent rules version")
			}
			if err := identity.Prepare(watchDir, rootStorePath, extract(subStorePath)); err != nil {
				log.Fatalf("prepare non-root reloader: %v", err)
			}
			startServer()
		},
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			UnknownFlags: true,
		},
	}

	rootCmd.Flags().StringVar(&listenAddress, "web.listen-address", "127.0.0.1:9089", "HTTP listen address; remote listeners require authentication and TLS.")
	rootCmd.Flags().StringVar(&authFile, "web.auth-file", "", "Mounted JSON file containing bcrypt password hashes and viewer/admin roles.")
	rootCmd.Flags().StringVar(&tlsCertFile, "web.tls-cert-file", "", "TLS certificate file.")
	rootCmd.Flags().StringVar(&tlsKeyFile, "web.tls-key-file", "", "TLS private-key file.")
	rootCmd.Flags().BoolVar(&allowInsecure, "web.allow-insecure", false, "Allow authenticated plaintext on a non-loopback address behind a trusted TLS proxy.")
	rootCmd.Flags().StringVar(&watchDir, "watch-path", "", "the directory to watch for updates.")
	rootCmd.Flags().StringVar(&rootStorePath, "root-store-path", "/data", "the directory to store updated files.")
	rootCmd.Flags().StringVar(&subStorePath, "sub-store-path", "", "the sub directory to store updated files.")
	rootCmd.Flags().StringVar(&promUrl, "prometheus-url", "http://localhost:9090", "the url to send a request to when the files are updated.")
	rootCmd.MarkFlagRequired("watch-path")

	rootCmd.Execute()
}

func startServer() {
	gin.SetMode(gin.ReleaseMode)
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	policy, err := auth.Load(authFile)
	if err != nil {
		log.Fatal(err)
	}
	if err := validateWebOptions(listenAddress, tlsCertFile, tlsKeyFile, policy != nil, allowInsecure); err != nil {
		log.Fatal(err)
	}
	if policy == nil {
		log.Print("administrative API disabled: configure --web.auth-file to enable access")
	}

	u, err := url.Parse(promUrl)
	if err != nil {
		panic(errors.Wrap(err, "invalid url"))
	}

	storePath := fmt.Sprintf("%s%clatest-rules%c%s", rootStorePath, filepath.Separator, filepath.Separator, extract(subStorePath))
	if len(subStorePath) != 0 && !exist(storePath) {
		log.Println("need to store latest file to store path")
		if err := os.MkdirAll(storePath, 0755); err != nil {
			log.Println("failed to create store path", err)
		}
	}

	reloader := bizlogic.NewReloader(u, watchDir)
	engine := apis.NewSecuredService(u, watchDir, len(subStorePath) != 0, storePath, policy, reloader)
	statikFS, err := fs.New()
	if err != nil {
		log.Fatal("failed to embed static files into your binary", err)
	}

	staticHandler := http.FileServer(statikFS)
	engine.NoRoute(func(c *gin.Context) {
		staticHandler.ServeHTTP(c.Writer, c.Request)
	})

	if err := reloader.Start(); err != nil {
		log.Fatal("Watch dir failed", err)
	}

	server := &http.Server{Addr: listenAddress, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}, Handler: engine, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	if tlsCertFile != "" {
		log.Fatal("StartServer server failed", server.ListenAndServeTLS(tlsCertFile, tlsKeyFile))
	}
	log.Fatal("StartServer server failed", server.ListenAndServe())
}

func extract(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == filepath.Separator {
			return path[i+1:]
		}
	}
	return path
}

func exist(filepath string) bool {
	if _, err := os.Stat(filepath); err != nil {
		return false
	}

	return true
}
