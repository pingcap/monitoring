module github.com/pingcap/monitoring

go 1.12

require (
	github.com/fsnotify/fsnotify v1.4.7
	github.com/gin-gonic/gin v1.4.0
	github.com/go-kit/kit v0.9.0 // indirect
	github.com/google/go-querystring v1.0.0
	github.com/hashicorp/go-version v1.2.0
	github.com/opentracing/opentracing-go v1.1.0 // indirect
	github.com/pkg/errors v0.8.1
	github.com/prometheus/common v0.4.1
	github.com/prometheus/prometheus v0.0.0-20190710134608-e5b22494857d
	github.com/rakyll/statik v0.1.6
	github.com/shurcooL/httpfs v0.0.0-20190707220628-8d4bc4ba7749
	github.com/shurcooL/vfsgen v0.0.0-20181202132449-6a9ea43bcacd
	github.com/spf13/cobra v0.0.5
	github.com/tidwall/gjson v1.3.2
	github.com/tidwall/sjson v1.0.4
	github.com/wushilin/stream v0.0.0-20160517090247-4c9093559eef
	gopkg.in/src-d/go-git.v4 v4.12.0
	gopkg.in/yaml.v2 v2.2.2
	k8s.io/apiextensions-apiserver v0.0.0-20190721103949-a013b2d4e1dd // indirect
	k8s.io/client-go v12.0.0+incompatible // indirect
)

replace github.com/ugorji/go v1.1.4 => github.com/ugorji/go/codec v0.0.0-20190204201341-e444a5086c43
