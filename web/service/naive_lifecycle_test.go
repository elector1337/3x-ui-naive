package service

import (
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/database/model"
	"github.com/mhsanaei/3x-ui/v3/util/crypto"
)

// buildMockCaddy compiles the in-tree mock_caddy binary and returns its path.
// The mock listens on the configured port with a self-signed TLS cert so the
// panel's TCP and HTTPS probes both succeed.
func buildMockCaddy(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "mock_caddy")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, "./testdata/mock_caddy")
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build mock_caddy: %v\n%s", err, data)
	}
	return out
}

// freePort grabs an ephemeral TCP port for the test to bind. Listener is
// closed immediately so the mock can take it over; tests on busy machines
// might race here, but in practice it's reliable enough for CI.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(40 * time.Millisecond)
	}
	return cond()
}

func TestNaiveLifecycle_Integration(t *testing.T) {
	setupConflictDB(t)
	crypto.SetEncryptionKeyPath(filepath.Join(t.TempDir(), "encryption.key"))

	mock := buildMockCaddy(t)
	t.Setenv("CADDY_BIN", mock)
	t.Setenv("XUI_BIN_FOLDER", t.TempDir())

	svc := &NaiveService{procs: make(map[int]*naiveProc)}
	port := freePort(t)

	srv := &model.NaiveServer{
		Remark:   "lifecycle",
		Enable:   true,
		Port:     port,
		Domain:   "lifecycle.example.com",
		CertFile: "/dev/null",
		KeyFile:  "/dev/null",
		AuthUser: "u",
		AuthPass: "p",
		Padding:  false,
		LogLevel: "WARN",
	}

	// === Add ===
	if err := svc.Add(srv); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if srv.Id == 0 {
		t.Fatal("Add did not set id")
	}

	// === Start ===
	if err := svc.Start(srv.Id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(srv.Id) })

	// pid set immediately
	st := svc.Status(srv.Id)
	if !st.Running || st.Pid == 0 {
		t.Fatalf("expected running with pid, got %+v", st)
	}

	// wait for TCP socket open
	if !waitFor(3*time.Second, func() bool { return svc.Status(srv.Id).Listening }) {
		t.Fatalf("port %d never opened", port)
	}

	// HTTPS probe: mock serves TLS so this should also flip true
	if !waitFor(3*time.Second, func() bool { return svc.Status(srv.Id).Responding }) {
		t.Fatalf("HTTPS probe never succeeded on port %d", port)
	}

	// raw verify: client connection through the panel's view
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 500*time.Millisecond)
	if err != nil {
		t.Errorf("direct dial to mock failed: %v", err)
	} else {
		_ = conn.Close()
	}

	// === Stop ===
	if err := svc.Stop(srv.Id); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// wait for reaper to clear the entry
	if !waitFor(3*time.Second, func() bool { return !svc.Status(srv.Id).Running }) {
		t.Fatalf("process never reaped: %+v", svc.Status(srv.Id))
	}
	st = svc.Status(srv.Id)
	if st.Running || st.Listening || st.Responding {
		t.Errorf("status after stop should be all false, got %+v", st)
	}

	// === Restart === just exercise the path
	if err := svc.Restart(srv.Id); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !waitFor(3*time.Second, func() bool { return svc.Status(srv.Id).Listening }) {
		t.Fatalf("port did not reopen after Restart")
	}
	_ = svc.Stop(srv.Id)
}

func TestNaiveRestore_ParallelStartsAllEnabledRows(t *testing.T) {
	setupConflictDB(t)
	crypto.SetEncryptionKeyPath(filepath.Join(t.TempDir(), "encryption.key"))

	mock := buildMockCaddy(t)
	t.Setenv("CADDY_BIN", mock)
	t.Setenv("XUI_BIN_FOLDER", t.TempDir())

	svc := &NaiveService{procs: make(map[int]*naiveProc)}
	const n = 3
	ports := make([]int, n)
	ids := make([]int, n)
	for i := 0; i < n; i++ {
		ports[i] = freePort(t)
		srv := &model.NaiveServer{
			Remark:   "r" + strconv.Itoa(i),
			Enable:   true,
			Port:     ports[i],
			Domain:   "x.example.com",
			CertFile: "/dev/null",
			KeyFile:  "/dev/null",
			AuthUser: "u",
			AuthPass: "p",
		}
		if err := svc.Add(srv); err != nil {
			t.Fatalf("Add #%d: %v", i, err)
		}
		ids[i] = srv.Id
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_ = svc.Stop(id)
		}
	})

	start := time.Now()
	svc.Restore()
	dur := time.Since(start)
	t.Logf("Restore took %v for %d servers", dur, n)

	for i, id := range ids {
		if !waitFor(3*time.Second, func() bool { return svc.Status(id).Listening }) {
			t.Errorf("server %d (port %d) did not become listening", i, ports[i])
		}
	}
}
