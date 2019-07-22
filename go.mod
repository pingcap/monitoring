module github.com/pingcap/monitoring

go 1.12

require (
	github.com/fsnotify/fsnotify v1.4.7
	github.com/gin-gonic/gin v1.4.0
	github.com/hashicorp/go-version v1.2.0
	github.com/pkg/errors v0.8.1
	github.com/prometheus/prometheus v2.5.0+incompatible // indirect
	github.com/rakyll/statik v0.1.6
	github.com/shurcooL/httpfs v0.0.0-20190707220628-8d4bc4ba7749
	github.com/shurcooL/vfsgen v0.0.0-20181202132449-6a9ea43bcacd
	github.com/spf13/cobra v0.0.5
	github.com/tidwall/gjson v1.3.2
	github.com/tidwall/sjson v1.0.4
	github.com/wushilin/stream v0.0.0-20160517090247-4c9093559eef
	gopkg.in/src-d/go-git.v4 v4.12.0
	gopkg.in/yaml.v2 v2.2.2
)

replace github.com/ugorji/go v1.1.4 => github.com/ugorji/go/codec v0.0.0-20190204201341-e444a5086c43
