package service

import (
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/database"
	"github.com/mhsanaei/3x-ui/v3/database/model"
)

func seedNaive(t *testing.T, listen string, port int) *model.NaiveServer {
	t.Helper()
	srv := &model.NaiveServer{
		Remark: "seed",
		Listen: listen,
		Port:   port,
		Domain: "naive.example.com",
		// any valid set so validate passes if reused
		AuthUser: "u", AuthPass: "p", CertFile: "a", KeyFile: "b",
	}
	if err := database.GetDB().Create(srv).Error; err != nil {
		t.Fatalf("seed naive: %v", err)
	}
	return srv
}

func TestCrossCheck_NaiveCollidesWithXrayInbound(t *testing.T) {
	setupConflictDB(t)

	// existing vless+tcp inbound on :443
	streamTCP := `{"network":"tcp"}`
	seedInboundConflict(t, "vless-1", "", 443, model.VLESS, streamTCP, "")

	err := crossCheckNaivePort(&model.NaiveServer{Port: 443, Domain: "x", AuthUser: "u", AuthPass: "p", CertFile: "a", KeyFile: "b"}, 0)
	if err == nil || !strings.Contains(err.Error(), "xray inbound") {
		t.Fatalf("expected xray-collision error, got %v", err)
	}
}

func TestCrossCheck_NaiveCollidesWithAnotherNaive(t *testing.T) {
	setupConflictDB(t)
	existing := seedNaive(t, "", 18443)

	err := crossCheckNaivePort(&model.NaiveServer{Port: 18443, Domain: "x", AuthUser: "u", AuthPass: "p", CertFile: "a", KeyFile: "b"}, 0)
	if err == nil || !strings.Contains(err.Error(), "naive server") {
		t.Fatalf("expected naive-collision error, got %v", err)
	}
	// updating the same row by id should be allowed
	if err := crossCheckNaivePort(&model.NaiveServer{Port: 18443, Domain: "x", AuthUser: "u", AuthPass: "p", CertFile: "a", KeyFile: "b"}, existing.Id); err != nil {
		t.Errorf("update on same id should not collide with itself: %v", err)
	}
}

func TestCrossCheck_DifferentListenInterfacesDoNotCollide(t *testing.T) {
	setupConflictDB(t)
	seedNaive(t, "127.0.0.1", 18443)

	// new server bound to 10.0.0.1:18443 — different interface, no collision
	if err := crossCheckNaivePort(&model.NaiveServer{Port: 18443, Listen: "10.0.0.1", Domain: "x", AuthUser: "u", AuthPass: "p", CertFile: "a", KeyFile: "b"}, 0); err != nil {
		t.Errorf("different interfaces should not collide: %v", err)
	}
}

func TestCrossCheck_AnyListenCollidesWithSpecific(t *testing.T) {
	setupConflictDB(t)
	seedNaive(t, "127.0.0.1", 18443)

	// new bound to 0.0.0.0 (bind-all) → overlaps with the existing 127.0.0.1
	err := crossCheckNaivePort(&model.NaiveServer{Port: 18443, Listen: "", Domain: "x", AuthUser: "u", AuthPass: "p", CertFile: "a", KeyFile: "b"}, 0)
	if err == nil {
		t.Errorf("bind-all should collide with specific-interface")
	}
}

func TestInboundCheck_FlagsExistingNaivePort(t *testing.T) {
	setupConflictDB(t)
	seedNaive(t, "", 443)

	svc := InboundService{}
	conflict, err := svc.checkPortConflict(&model.Inbound{
		Port:           443,
		Listen:         "",
		Protocol:       model.VLESS,
		StreamSettings: `{"network":"tcp"}`,
	}, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !conflict {
		t.Errorf("xray inbound on :443 should conflict with existing naive on :443")
	}
}

func TestInboundCheck_UDPInboundIgnoresTCPNaive(t *testing.T) {
	setupConflictDB(t)
	seedNaive(t, "", 443)

	svc := InboundService{}
	// hysteria2 = pure UDP, doesn't collide with naive's TCP socket
	conflict, err := svc.checkPortConflict(&model.Inbound{
		Port:     443,
		Listen:   "",
		Protocol: model.Hysteria2,
	}, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if conflict {
		t.Errorf("UDP-only inbound should not conflict with TCP naive")
	}
}
