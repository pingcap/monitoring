package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLI(t *testing.T) {
	if os.Getenv("MONITORING_CLI_TEST") == "1" {
		os.Args = append([]string{"load"}, flag.Args()...)
		main()
		return
	}
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	for _, tc := range []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{"unknown flag", []string{"--config=" + missing, "--auto-puhs"}, "unknown flag: --auto-puhs", false},
		{"unknown flag before required flag", []string{"--unknown", "--config=" + missing}, "unknown flag: --unknown", false},
		{"missing required flag", nil, `required flag(s) "config" not set`, false},
		{"missing value", []string{"--config"}, "flag needs an argument", false},
		{"invalid boolean", []string{"--config=" + missing, "--auto-push=maybe"}, "invalid argument", false},
		{"positional argument", []string{"--config=" + missing, "unexpected"}, "unknown command", false},
		{"positional after separator", []string{"--config=" + missing, "--", "unexpected"}, "unknown command", false},
		{"help", []string{"--help"}, "Usage:", true},
		{"build script flags", []string{"--config=monitoring.yaml", "--tag=v8.5.7", "--token=", "--help"}, "Usage:", true},
		{"all supported flags", []string{"--config", "monitoring.yaml", "--tag", "v8.5.7", "--token=local-test", "--output-dir", missing, "--auto-push=false", "--help"}, "Usage:", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLI$", "--"}, tc.args...)...)
			cmd.Env = append(os.Environ(), "MONITORING_CLI_TEST=1")
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || (err == nil) != tc.ok || !strings.Contains(string(out), tc.want) {
				t.Fatalf("exit error: %v; timeout: %v; want success=%t and %q; output:\n%s", err, ctx.Err(), tc.ok, tc.want, out)
			}
		})
	}
}
