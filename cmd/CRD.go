package main

import (
	"fmt"
	_"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"net/url"
	"k8s.io/client-go/rest"
	//apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
)

var(
	nameSpace string
	ruleResourceName string
	ruleName string
	host                          string
	tls_insecure                   bool
	tls_config                     rest.TLSClientConfig
)

func main() {
	var rootCmd = &cobra.Command{
		Use: "crd",
		Run: func(co *cobra.Command, args []string) {

		},
	}

	rootCmd.Flags().StringVar(&nameSpace,"namespace", "", "the namespace name.")
	rootCmd.Flags().StringVar( &ruleResourceName,"resource-name", "", "the crd name of alert rule.")
	rootCmd.Flags().StringVar(&ruleName, "rule-name", "", "the rule name.")

	rootCmd.Flags().StringVar(&tls_config.CertFile, "cert-file", "", " - NOT RECOMMENDED FOR PRODUCTION - Path to public TLS certificate file.")
	rootCmd.Flags().StringVar(&tls_config.KeyFile, "key-file", "", "- NOT RECOMMENDED FOR PRODUCTION - Path to private TLS certificate file.")
	rootCmd.Flags().StringVar(&tls_config.CAFile, "ca-file", "", "- NOT RECOMMENDED FOR PRODUCTION - Path to TLS CA file.")
	rootCmd.Flags().BoolVar(&tls_insecure, "tls-insecure", false, "- NOT RECOMMENDED FOR PRODUCTION - Don't verify API server's CA certificate.")
	rootCmd.Flags().StringVar(&host, "apiserver", "", "API Server addr, e.g. ' - NOT RECOMMENDED FOR PRODUCTION - http://127.0.0.1:8080'. Omit parameter to run in on-cluster mode and utilize the service account token.")

	rootCmd.MarkFlagRequired("namespace")
	rootCmd.MarkFlagRequired("resource-name")
	rootCmd.MarkFlagRequired("rule-name")
}

//func AppalyCRD() error{
//	cfg, err := NewClusterConfig(host, tls_insecure, &tls_config)
//	if err != nil {
//		return errors.Wrap(err, "instantiating cluster config failed")
//	}
//
//	crdclient, err := apiextensionsclient.NewForConfig(cfg)
//	if err != nil {
//		return errors.Wrap(err, "instantiating apiextensions client failed")
//	}
//
//	r := crdclient.ApiextensionsV1beta1().RESTClient().Get().Namespace(nameSpace).Resource(ruleResourceName).Name(ruleName).Do()
//
//	crdclient.ApiextensionsV1beta1().RESTClient().Post().Namespace(nameSpace).Resource(ruleResourceName).Name(ruleName).Body()
//	r.StatusCode()
//}

func NewClusterConfig(host string, tlsInsecure bool, tlsConfig *rest.TLSClientConfig) (*rest.Config, error) {
	var cfg *rest.Config
	var err error

	if len(host) == 0 {
		if cfg, err = rest.InClusterConfig(); err != nil {
			return nil, err
		}
	} else {
		cfg = &rest.Config{
			Host: host,
		}
		hostURL, err := url.Parse(host)
		if err != nil {
			return nil, fmt.Errorf("error parsing host url %s : %v", host, err)
		}
		if hostURL.Scheme == "https" {
			cfg.TLSClientConfig = *tlsConfig
			cfg.Insecure = tlsInsecure
		}
	}
	cfg.QPS = 100
	cfg.Burst = 100

	return cfg, nil
}