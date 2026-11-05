package main

import "testing"

func TestWebListenerSecurity(t *testing.T) {
	for _, tc := range []struct {
		name, address, cert, key      string
		configured, insecure, wantErr bool
	}{
		{name: "unconfigured loopback stays closed", address: "127.0.0.1:9089"},
		{name: "IPv6 loopback", address: "[::1]:9089", configured: true},
		{name: "anonymous remote", address: "0.0.0.0:9089", wantErr: true},
		{name: "plaintext remote", address: "0.0.0.0:9089", configured: true, wantErr: true},
		{name: "TLS remote", address: "0.0.0.0:9089", cert: "cert", key: "key", configured: true},
		{name: "explicit trusted proxy", address: "0.0.0.0:9089", configured: true, insecure: true},
		{name: "missing key", address: "127.0.0.1:9089", cert: "cert", configured: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWebOptions(tc.address, tc.cert, tc.key, tc.configured, tc.insecure)
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}
