package main

import (
	"fmt"
	"net"
)

func validateWebOptions(address, cert, key string, configured, allowInsecure bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid web listen address")
	}
	if (cert == "") != (key == "") {
		return fmt.Errorf("both TLS certificate and key are required")
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if !loopback {
		if !configured {
			return fmt.Errorf("non-loopback listener requires an authentication file")
		}
		if cert == "" && !allowInsecure {
			return fmt.Errorf("non-loopback listener requires TLS; allow insecure only behind a trusted TLS proxy")
		}
	}
	return nil
}
