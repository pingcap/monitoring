package ansible

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDashboardPackagingArchiveLayouts(t *testing.T) {
	for _, prefix := range []string{"ansible/", "/ansible/"} {
		for _, platform := range []string{"Darwin", "Linux"} {
			t.Run(platform+prefix, func(t *testing.T) {
				root := t.TempDir()
				for _, dir := range []string{"scripts", "bin", "monitor-snapshot/test/operator/dashboards"} {
					if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
						t.Fatal(err)
					}
				}
				script, err := os.ReadFile("../../scripts/prepare_dashboards.sh")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "scripts/prepare_dashboards.sh"), script, 0644); err != nil {
					t.Fatal(err)
				}
				// The extraction helper is added by the fix; baseline still exercises the old script.
				if helper, err := os.ReadFile("../../scripts/extract_dashboard_archive.sh"); err == nil {
					if err := os.WriteFile(filepath.Join(root, "scripts/extract_dashboard_archive.sh"), helper, 0644); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(root, "bin/uname"), []byte("#!/bin/sh\necho "+platform+"\n"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "monitor-snapshot/test/operator/dashboards/operator.json"), []byte("operator"), 0644); err != nil {
					t.Fatal(err)
				}
				f, err := os.Create(filepath.Join(root, "monitor-snapshot/test/ansible-monitor.tar.gz"))
				if err != nil {
					t.Fatal(err)
				}
				gz := gzip.NewWriter(f)
				tw := tar.NewWriter(gz)
				data := []byte("dashboard")
				if err := tw.WriteHeader(&tar.Header{Name: prefix + "tidb-monitor/dashboard.json", Mode: 0644, Size: int64(len(data))}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write(data); err != nil {
					t.Fatal(err)
				}
				for _, closer := range []interface{ Close() error }{tw, gz, f} {
					if err := closer.Close(); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command("bash", "scripts/prepare_dashboards.sh")
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "NOPULL=1", "TARGET=test", "PATH="+filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("packaging failed: %v\n%s", err, out)
				}
				for name, want := range map[string]string{"tiup/dashboard.json": "dashboard", "operator/operator.json": "operator"} {
					got, err := os.ReadFile(filepath.Join(root, "output/dashboards", name))
					if err != nil || string(got) != want {
						t.Fatalf("bad packaged %s: %q %v", name, got, err)
					}
				}
			})
		}
	}
}
