package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestSecuredServerProcess(t *testing.T) {
	if os.Getenv("RELOADER_TLS_TEST") != "1" {
		t.Skip("subprocess helper")
	}
	listenAddress = os.Getenv("RELOADER_TLS_ADDRESS")
	authFile = os.Getenv("RELOADER_TLS_AUTH")
	tlsCertFile = os.Getenv("RELOADER_TLS_CERT")
	tlsKeyFile = os.Getenv("RELOADER_TLS_KEY")
	watchDir = os.Getenv("RELOADER_TLS_DIR")
	promUrl = "http://127.0.0.1:9090"
	startServer()
}

func TestRealTLSListenerRequiresAuthentication(t *testing.T) {
	root := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath, authPath := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem"), filepath.Join(root, "auth.json")
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("local-test-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"users": map[string]any{"admin": map[string]string{"password_hash": string(hash), "role": "admin"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rules.yml"), []byte("groups: []"), 0644); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSecuredServerProcess$")
	cmd.Env = append(os.Environ(), "RELOADER_TLS_TEST=1", "RELOADER_TLS_ADDRESS="+address, "RELOADER_TLS_AUTH="+authPath, "RELOADER_TLS_CERT="+certPath, "RELOADER_TLS_KEY="+keyPath, "RELOADER_TLS_DIR="+root)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); cmd.Wait() }()
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	defer client.CloseIdleConnections()
	endpoint := "https://" + address + "/monitoring/configs"
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := client.Get(endpoint)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatal("anonymous TLS access accepted", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("TLS listener did not start", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	req.SetBasicAuth("admin", "local-test-password")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated TLS request failed: %d %s %v", resp.StatusCode, data, err)
	}
}
