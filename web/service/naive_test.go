package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/database/model"
)

func TestWriteNaiveConfig_BasicCaddyfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.caddyfile")

	srv := &model.NaiveServer{
		Listen:   "0.0.0.0",
		Port:     8443,
		Domain:   "naive.example.com",
		CertFile: "/etc/ssl/cert.pem",
		KeyFile:  "/etc/ssl/key.pem",
		AuthUser: "alice",
		AuthPass: "s3cret",
		Padding:  true,
		LogLevel: "WARN",
	}

	if err := writeNaiveConfig(path, srv); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	out := string(b)

	for _, want := range []string{
		"admin off",
		"level WARN",
		":8443, naive.example.com",
		"tls /etc/ssl/cert.pem /etc/ssl/key.pem",
		"forward_proxy",
		"basic_auth alice s3cret",
		"hide_ip",
		"hide_via",
		"probe_resistance",
		"padding",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("caddyfile missing %q\n--full--\n%s", want, out)
		}
	}
	// 0.0.0.0 should be normalized to bind-all (no explicit bind directive)
	if strings.Contains(out, "bind ") {
		t.Errorf("0.0.0.0 should not produce a bind directive\n%s", out)
	}
}

func TestWriteNaiveConfig_SpecificBind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.caddyfile")
	srv := &model.NaiveServer{
		Listen:   "127.0.0.1",
		Port:     8443,
		Domain:   "x.example.com",
		CertFile: "/a.pem",
		KeyFile:  "/b.pem",
		AuthUser: "u",
		AuthPass: "p",
	}
	if err := writeNaiveConfig(path, srv); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _ := os.ReadFile(path)
	out := string(b)
	if !strings.Contains(out, "bind 127.0.0.1") {
		t.Errorf("expected bind 127.0.0.1 for specific listen\n%s", out)
	}
}

func TestWriteNaiveConfig_NoPadding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.caddyfile")

	srv := &model.NaiveServer{
		Port:     443,
		Domain:   "x.example.com",
		CertFile: "/a.pem",
		KeyFile:  "/b.pem",
		AuthUser: "u",
		AuthPass: "p",
		Padding:  false,
	}
	if err := writeNaiveConfig(path, srv); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "padding\n") {
		t.Errorf("padding should not appear when disabled\n%s", string(b))
	}
}

func TestWriteNaiveConfig_RawMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.caddyfile")
	raw := ":443, naive.example.com {\n\ttls me@example.com\n\troute {\n\t\tfile_server\n\t}\n}\n"
	srv := &model.NaiveServer{
		UseRawConfig: true,
		RawConfig:    raw,
	}
	if err := writeNaiveConfig(path, srv); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != raw {
		t.Errorf("raw config not written verbatim\ngot:\n%s\nwant:\n%s", string(b), raw)
	}
}

func TestValidateNaive_RawMode(t *testing.T) {
	if err := validateNaive(&model.NaiveServer{UseRawConfig: true, RawConfig: "  "}); err == nil {
		t.Errorf("empty raw should fail")
	}
	if err := validateNaive(&model.NaiveServer{UseRawConfig: true, RawConfig: "anything"}); err != nil {
		t.Errorf("non-empty raw should pass, got %v", err)
	}
}

func TestRenderCaddyfile_ReturnsRawWhenUsed(t *testing.T) {
	out := RenderCaddyfile(&model.NaiveServer{UseRawConfig: true, RawConfig: "hello\n"})
	if out != "hello\n" {
		t.Errorf("expected raw passthrough, got %q", out)
	}
}

func TestRenderCaddyfile_RendersFromForm(t *testing.T) {
	out := RenderCaddyfile(&model.NaiveServer{
		Port: 443, Domain: "x.example.com", CertFile: "a", KeyFile: "b",
		AuthUser: "u", AuthPass: "p",
	})
	if !strings.Contains(out, "forward_proxy") || !strings.Contains(out, ":443, x.example.com") {
		t.Errorf("expected generated Caddyfile, got:\n%s", out)
	}
}

func TestWriteNaiveConfig_ACME(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.caddyfile")
	srv := &model.NaiveServer{
		Port:      443,
		Domain:    "naive.example.com",
		AuthUser:  "u",
		AuthPass:  "p",
		UseACME:   true,
		AcmeEmail: "me@example.com",
	}
	if err := writeNaiveConfig(path, srv); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _ := os.ReadFile(path)
	out := string(b)
	if !strings.Contains(out, "tls me@example.com") {
		t.Errorf("expected ACME tls directive\n%s", out)
	}
	if strings.Contains(out, ".pem") {
		t.Errorf("ACME mode should not reference cert/key files\n%s", out)
	}
}

func TestWriteNaiveConfig_LegacyLogLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.caddyfile")

	srv := &model.NaiveServer{
		Port: 443, Domain: "x", CertFile: "/a", KeyFile: "/b",
		AuthUser: "u", AuthPass: "p", LogLevel: "WARNING",
	}
	if err := writeNaiveConfig(path, srv); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _ := os.ReadFile(path)
	out := string(b)
	if !strings.Contains(out, "level WARN") || strings.Contains(out, "level WARNING") {
		t.Errorf("legacy WARNING should be normalized to WARN\n%s", out)
	}
}

func TestValidateNaive(t *testing.T) {
	cases := []struct {
		name string
		srv  *model.NaiveServer
		ok   bool
	}{
		{"nil", nil, false},
		{"bad port", &model.NaiveServer{Port: 0, Domain: "x", AuthUser: "u", AuthPass: "p", CertFile: "a", KeyFile: "b"}, false},
		{"no domain", &model.NaiveServer{Port: 443, AuthUser: "u", AuthPass: "p", CertFile: "a", KeyFile: "b"}, false},
		{"no auth", &model.NaiveServer{Port: 443, Domain: "x", CertFile: "a", KeyFile: "b"}, false},
		{"no cert", &model.NaiveServer{Port: 443, Domain: "x", AuthUser: "u", AuthPass: "p"}, false},
		{"valid manual", &model.NaiveServer{Port: 443, Domain: "x", AuthUser: "u", AuthPass: "p", CertFile: "a", KeyFile: "b"}, true},
		{"acme no email", &model.NaiveServer{Port: 443, Domain: "x", AuthUser: "u", AuthPass: "p", UseACME: true}, false},
		{"valid acme", &model.NaiveServer{Port: 443, Domain: "x", AuthUser: "u", AuthPass: "p", UseACME: true, AcmeEmail: "me@example.com"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNaive(tc.srv)
			if tc.ok && err != nil {
				t.Errorf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Errorf("expected error, got nil")
			}
		})
	}
}
