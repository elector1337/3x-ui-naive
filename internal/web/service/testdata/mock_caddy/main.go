// mock_caddy is a tiny stand-in for the real Caddy binary used in integration
// tests. It mimics the CLI surface naive.go needs:
//   - `mock_caddy version`              — prints a fake version string
//   - `mock_caddy run --config X.caddyfile --adapter caddyfile` — listens on the
//     port found in the Caddyfile until SIGTERM
//   - `mock_caddy adapt --config X.caddyfile --adapter caddyfile` — succeeds
//
// It also speaks rudimentary HTTPS using a self-signed cert so the panel's
// HTTPS HEAD probe succeeds.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(0)
	}
	sub := os.Args[1]
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	config := flag.String("config", "", "")
	_ = flag.String("adapter", "", "")
	flag.Parse()

	switch sub {
	case "version":
		fmt.Println("v2.0.0-mock h1:mocksha=")
		return
	case "adapt":
		// pretend everything adapts successfully
		fmt.Println("{}")
		return
	case "run":
		runServer(*config)
	default:
		// unknown command — just exit OK
	}
}

func runServer(cfgPath string) {
	port := 0
	if cfgPath != "" {
		if b, err := os.ReadFile(cfgPath); err == nil {
			if m := regexp.MustCompile(`:(\d{2,5})[,\s]`).FindSubmatch(b); len(m) == 2 {
				port, _ = strconv.Atoi(string(m[1]))
			}
		}
	}
	if port == 0 {
		// no port: just sleep until killed
		blockUntilSignal()
		return
	}

	cert, key := makeSelfSignedCert()
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := tls.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), tlsCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mock_caddy: bind: %v\n", err)
		os.Exit(1)
	}
	_ = key // unused after cert assembly

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, "mock_caddy ok")
		}),
		ReadHeaderTimeout: 2 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		_ = ln.Close()
	}()

	_ = srv.Serve(ln)
}

func blockUntilSignal() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
}

// makeSelfSignedCert returns a freshly generated short-lived self-signed cert
// for 127.0.0.1. The HEAD probe uses InsecureSkipVerify, so the CA chain
// doesn't matter — only that the TLS handshake completes.
func makeSelfSignedCert() (tls.Certificate, *ecdsa.PrivateKey) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}, key
}
