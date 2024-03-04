module github.com/pingcap/monitoring

go 1.12

require (
	github.com/fsnotify/fsnotify v1.4.7
	github.com/gin-gonic/gin v1.9.1
	github.com/go-kit/kit v0.9.0 // indirect
	github.com/google/go-github v17.0.0+incompatible
	github.com/google/go-querystring v1.0.0
	github.com/hashicorp/go-version v1.2.1
	github.com/opentracing/opentracing-go v1.1.0 // indirect
	github.com/pkg/errors v0.8.1
	github.com/prometheus/common v0.4.1
	github.com/prometheus/prometheus v0.0.0-20190710134608-e5b22494857d
	github.com/rakyll/statik v0.1.6
	github.com/sirupsen/logrus v1.4.2 // indirect
	github.com/spf13/cobra v0.0.5
	github.com/tidwall/gjson v1.9.3
	github.com/tidwall/sjson v1.0.4
	github.com/youthlin/stream v0.0.3
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	gopkg.in/src-d/go-git.v4 v4.12.0
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.0.0-20190720062849-3043179095b6 // indirect
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/utils v0.0.0-20190607212802-c55fbcfc754a // indirect
)

replace github.com/ugorji/go v1.1.4 => github.com/ugorji/go/codec v0.0.0-20190204201341-e444a5086c43
