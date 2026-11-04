package operator

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPodDashboardStorageQueriesAreNamespaced(t *testing.T) {
	for _, file := range []string{"../../cmd/dashboards/pods/pods.json", "../../platform-monitoring/operator/grafana/pods.json", "../../monitor-snapshot/master/operator/dashboards/pods.json"} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var dashboard any
			if err := json.Unmarshal(data, &dashboard); err != nil {
				t.Fatal(err)
			}
			count := 0
			var visit func(any)
			visit = func(value any) {
				switch v := value.(type) {
				case map[string]any:
					if expr, ok := v["expr"].(string); ok && (strings.Contains(expr, "kubelet_volume_stats_") || strings.Contains(expr, "container_fs_")) {
						count++
						if !strings.Contains(expr, `namespace="$namespace"`) {
							t.Errorf("query lacks namespace filter: %s", expr)
						}
					}
					for _, child := range v {
						visit(child)
					}
				case []any:
					for _, child := range v {
						visit(child)
					}
				}
			}
			visit(dashboard)
			if count != 5 {
				t.Fatalf("expected five storage queries, got %d", count)
			}
		})
	}
}
