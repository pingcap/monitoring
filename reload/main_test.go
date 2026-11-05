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
	if os.Getenv("RELOADER_CLI_TEST") == "1" {
		os.Args = append([]string{"reload"}, flag.Args()...)
		main()
		return
	}
	missing := filepath.Join(t.TempDir(), "missing")
	for _, tc := range []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{"unknown security flag", []string{"--watch-path=" + missing, "--web.auth-fiel=auth.json"}, "unknown flag: --web.auth-fiel", false},
		{"unknown flag before required flag", []string{"--unknown", "--watch-path=" + missing}, "unknown flag: --unknown", false},
		{"missing required flag", nil, `required flag(s) "watch-path" not set`, false},
		{"missing value", []string{"--watch-path"}, "flag needs an argument", false},
		{"invalid boolean", []string{"--watch-path=" + missing, "--web.allow-insecure=maybe"}, "invalid argument", false},
		{"positional argument", []string{"--watch-path=" + missing, "unexpected"}, "unknown command", false},
		{"positional after separator", []string{"--watch-path=" + missing, "--", "unexpected"}, "unknown command", false},
		{"help", []string{"--help"}, "Usage:", true},
		{"operator and security flags", []string{"--watch-path=/prometheus-rules/rules", "--root-store-path=/data", "--sub-store-path=tidb:v8.5.7", "--prometheus-url=http://127.0.0.1:9090", "--web.listen-address=0.0.0.0:9089", "--web.auth-file=/web/auth.json", "--web.tls-cert-file=/web/tls.crt", "--web.tls-key-file=/web/tls.key", "--web.allow-insecure=false", "--help"}, "Usage:", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLI$", "--"}, tc.args...)...)
			cmd.Env = append(os.Environ(), "RELOADER_CLI_TEST=1")
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || (err == nil) != tc.ok || !strings.Contains(string(out), tc.want) {
				t.Fatalf("exit error: %v; timeout: %v; want success=%t and %q; output:\n%s", err, ctx.Err(), tc.ok, tc.want, out)
			}
		})
	}
}
