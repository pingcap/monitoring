package statik_test

import (
	"io"
	"os"
	"testing"

	_ "github.com/pingcap/monitoring/reload/statik"
	"github.com/rakyll/statik/fs"
)

func TestEmbeddedRuleUIIsCurrent(t *testing.T) {
	assets, err := fs.New()
	if err != nil {
		t.Fatal(err)
	}
	f, err := assets.Open("/static/js/data.js")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../ui/static/js/data.js")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("embedded rule UI does not match source")
	}
}
