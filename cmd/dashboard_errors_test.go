package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pingcap/monitoring/pkg/common"
)

type dashboardResponseTransport func(*http.Request) (*http.Response, error)

func (f dashboardResponseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUnregisteredDashboardIsNotWritten(t *testing.T) {
	oldTransport, oldAnsible, oldOperator := http.DefaultTransport, ansibleGrafanaDir, operatorGrafanaDir
	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
		ansibleGrafanaDir = oldAnsible
		operatorGrafanaDir = oldOperator
	})
	ansibleGrafanaDir, operatorGrafanaDir = t.TempDir(), t.TempDir()
	http.DefaultTransport = dashboardResponseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"title":"Unregistered"}`))}, nil
	})
	service, err := common.NewGitRepoService()
	if err != nil {
		t.Fatal(err)
	}
	name, target := "unregistered.json", "https://raw.githubusercontent.com/pingcap/tidb/master/unregistered.json"
	err = ProcessDashboards([]*common.RepositoryContent{{Name: &name, DownloadURL: &target}}, service)
	if err == nil {
		t.Fatal("unregistered dashboard accepted")
	}
	for _, dir := range []string{ansibleGrafanaDir, operatorGrafanaDir} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("unregistered dashboard written to %s", dir)
		}
	}
}

func TestStartStopsOnDashboardRejection(t *testing.T) {
	oldTransport, oldConfig, oldAnsible, oldOperator := http.DefaultTransport, cfg, ansibleGrafanaDir, operatorGrafanaDir
	t.Cleanup(func() {
		http.DefaultTransport, cfg, ansibleGrafanaDir, operatorGrafanaDir = oldTransport, oldConfig, oldAnsible, oldOperator
	})
	ansibleGrafanaDir, operatorGrafanaDir = t.TempDir(), t.TempDir()
	cfg = &Config{ComponentConfigs: []ComponentConfig{{Owner: "pingcap", RepoName: "component", MonitorPath: "dashboards"}}}
	name, target := "unregistered.json", "https://raw.githubusercontent.com/pingcap/component/master/unregistered.json"
	listing, err := json.Marshal([]*common.RepositoryContent{{Name: &name, DownloadURL: &target}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	http.DefaultTransport = dashboardResponseTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var body string
		switch calls {
		case 1:
			if r.URL.Host != "api.github.com" {
				t.Fatal(r.URL)
			}
			body = string(listing)
		case 2:
			if r.URL.String() != target {
				t.Fatal(r.URL)
			}
			body = `{"title":"Unregistered"}`
		default:
			t.Fatal("generation continued after a rejected dashboard")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if err := Start(); err == nil {
		t.Fatal("Start ignored dashboard rejection")
	}
	if calls != 2 {
		t.Fatalf("expected list and download, got %d requests", calls)
	}
}

func TestRegisteredDashboardAndEmptyInput(t *testing.T) {
	oldTransport, oldAnsible, oldOperator := http.DefaultTransport, ansibleGrafanaDir, operatorGrafanaDir
	t.Cleanup(func() {
		http.DefaultTransport, ansibleGrafanaDir, operatorGrafanaDir = oldTransport, oldAnsible, oldOperator
	})
	ansibleGrafanaDir, operatorGrafanaDir = t.TempDir(), t.TempDir()
	http.DefaultTransport = dashboardResponseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"title":"TiDB"}`))}, nil
	})
	service, err := common.NewGitRepoService()
	if err != nil {
		t.Fatal(err)
	}
	if err := ProcessDashboards(nil, service); err == nil {
		t.Fatal("empty directory accepted")
	}
	name, target := "tidb.json", "https://raw.githubusercontent.com/pingcap/tidb/master/tidb.json"
	if err := ProcessDashboards([]*common.RepositoryContent{{Name: &name, DownloadURL: &target}}, service); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{ansibleGrafanaDir, operatorGrafanaDir} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}
