package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/config"
	"github.com/mhsanaei/3x-ui/v3/database"
	"github.com/mhsanaei/3x-ui/v3/database/model"
	"github.com/mhsanaei/3x-ui/v3/logger"

	"gorm.io/gorm"
)

var (
	ErrNaiveNotFound       = errors.New("naive server not found")
	ErrNaiveBinaryMissing  = errors.New("caddy binary with forward_proxy plugin not found (set CADDY_BIN or place 'caddy' in PATH)")
	ErrNaiveAlreadyRunning = errors.New("naive server already running")
	ErrNaiveInvalidConfig  = errors.New("invalid naive server config")
)

type NaiveStatus struct {
	Id         int    `json:"id"`
	Running    bool   `json:"running"`
	Pid        int    `json:"pid,omitempty"`
	Since      int64  `json:"since,omitempty"`
	LogPath    string `json:"logPath,omitempty"`
	Listening  bool   `json:"listening"`
	Responding bool   `json:"responding"`
}

type naiveProc struct {
	cmd     *exec.Cmd
	started time.Time
	logPath string
	port    int
}

type NaiveService struct {
	mu    sync.Mutex
	procs map[int]*naiveProc
}

// NewNaiveService constructs a fresh NaiveService. The web layer wires one
// instance into the controller and the boot-time Restore call so the rest
// of the code doesn't reach for a process-wide singleton.
func NewNaiveService() *NaiveService {
	return &NaiveService{procs: make(map[int]*naiveProc)}
}

func (s *NaiveService) List() ([]*model.NaiveServer, error) {
	var rows []*model.NaiveServer
	err := database.GetDB().Preload("Users").Order("id asc").Find(&rows).Error
	return rows, err
}

func (s *NaiveService) Get(id int) (*model.NaiveServer, error) {
	row := &model.NaiveServer{}
	if err := database.GetDB().Preload("Users").First(row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNaiveNotFound
		}
		return nil, err
	}
	return row, nil
}

// snapshotPlain captures the plaintext AuthPass and per-user passwords before a
// write (gorm BeforeSave encrypts them in place).
func snapshotPlain(srv *model.NaiveServer) (string, []string) {
	users := make([]string, len(srv.Users))
	for i, u := range srv.Users {
		if u != nil {
			users[i] = u.Password
		}
	}
	return srv.AuthPass, users
}

// roundTripAuthPass restores the plaintext AuthPass and user passwords after a
// write so callers (and JSON responses) keep seeing the original values — gorm
// BeforeSave encrypted the in-memory structs, which is correct for the DB but
// wrong for the API response.
func roundTripAuthPass(srv *model.NaiveServer, original string, userPlain []string) {
	if srv == nil {
		return
	}
	srv.AuthPass = original
	for i, u := range srv.Users {
		if u != nil && i < len(userPlain) {
			u.Password = userPlain[i]
		}
	}
}

func (s *NaiveService) Add(srv *model.NaiveServer) error {
	if err := validateNaive(srv); err != nil {
		return err
	}
	if !srv.UseRawConfig {
		if err := crossCheckNaivePort(srv, 0); err != nil {
			return err
		}
	}
	normalizeNaiveUsers(srv)
	plain, userPlain := snapshotPlain(srv)
	// gorm's full-association Create persists srv.Users too (NaiveId is filled in).
	if err := database.GetDB().Create(srv).Error; err != nil {
		return err
	}
	roundTripAuthPass(srv, plain, userPlain)
	s.syncTrafficCounters()
	return nil
}

func (s *NaiveService) Update(srv *model.NaiveServer) error {
	if err := validateNaive(srv); err != nil {
		return err
	}
	if !srv.UseRawConfig {
		if err := crossCheckNaivePort(srv, srv.Id); err != nil {
			return err
		}
	}
	normalizeNaiveUsers(srv)
	plain, userPlain := snapshotPlain(srv)
	// Replace the user set: delete existing rows, then recreate from payload.
	// Simpler and less error-prone than diffing, and the set is always small.
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(srv).Error; err != nil {
			return err
		}
		if err := tx.Where("naive_id = ?", srv.Id).Delete(&model.NaiveUser{}).Error; err != nil {
			return err
		}
		for _, u := range srv.Users {
			u.Id = 0
			u.NaiveId = srv.Id
			if err := tx.Create(u).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	roundTripAuthPass(srv, plain, userPlain)
	s.syncTrafficCounters()
	return nil
}

// normalizeNaiveUsers drops empty user rows (no username) and clears stale
// foreign keys / ids so they are (re)created cleanly under the parent server.
func normalizeNaiveUsers(srv *model.NaiveServer) {
	kept := srv.Users[:0]
	for _, u := range srv.Users {
		if u == nil || strings.TrimSpace(u.Username) == "" {
			continue
		}
		u.NaiveId = srv.Id
		kept = append(kept, u)
	}
	srv.Users = kept
}

func (s *NaiveService) Delete(id int) error {
	_ = s.Stop(id)
	err := database.GetDB().Delete(&model.NaiveServer{}, id).Error
	s.syncTrafficCounters()
	return err
}

// syncTrafficCounters rebuilds the kernel (nftables) counter table from the
// current set of servers. Called after any structural change (add/update/delete)
// and on boot. Best-effort: on non-Linux, without root, or without nft it is a
// no-op, and any error is logged rather than propagated — traffic accounting is
// auxiliary and must never block server management.
func (s *NaiveService) syncTrafficCounters() {
	rows, err := s.List()
	if err != nil {
		logger.Warningf("naive traffic: list for counter sync: %v", err)
		return
	}
	if len(rows) == 0 {
		// No servers left — remove the table entirely rather than leaving an
		// empty one behind.
		if err := teardownNftCounters(); err != nil {
			logger.Warningf("naive traffic: teardown counters: %v", err)
		}
		return
	}
	if err := rebuildNftCounters(rows); err != nil {
		logger.Warningf("naive traffic: rebuild counters: %v", err)
	}
}

// SampleTraffic reads-and-zeroes the kernel counters and accumulates the deltas
// into each server's Up/Down columns. Called periodically by the naive traffic
// cron job. No-op when nft is unavailable.
func (s *NaiveService) SampleTraffic() error {
	deltas, err := sampleNftCounters()
	if err != nil {
		return err
	}
	return s.applyTrafficDeltas(deltas)
}

// applyTrafficDeltas accumulates the given per-server byte deltas into the
// stored Up/Down columns. Split out from SampleTraffic so it can be tested
// without nft. No-op for an empty map.
func (s *NaiveService) applyTrafficDeltas(deltas map[int]naiveTrafficDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	return submitTrafficWrite(func() error {
		db := database.GetDB()
		for id, d := range deltas {
			if d.Up == 0 && d.Down == 0 {
				continue
			}
			// UpdateColumns (not Save) so the AuthPass BeforeSave encryption
			// hook and autoUpdateTime are both skipped — we only touch counters.
			if err := db.Model(&model.NaiveServer{}).Where("id = ?", id).
				UpdateColumns(map[string]any{
					"up":   gorm.Expr("up + ?", d.Up),
					"down": gorm.Expr("down + ?", d.Down),
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ResetTraffic zeroes the stored Up/Down counters for one server.
func (s *NaiveService) ResetTraffic(id int) error {
	return submitTrafficWrite(func() error {
		return database.GetDB().Model(&model.NaiveServer{}).Where("id = ?", id).
			UpdateColumns(map[string]any{"up": 0, "down": 0}).Error
	})
}

// EnforceQuotas stops any running server that has hit its traffic quota or
// passed its expiry time. The process is left stopped (Enable is untouched) so
// it comes back automatically once the quota is reset or the expiry extended.
// Best-effort: called by the naive traffic job after each sample.
func (s *NaiveService) EnforceQuotas() {
	rows, err := s.List()
	if err != nil {
		logger.Warningf("naive quota: list: %v", err)
		return
	}
	now := time.Now().UnixMilli()
	for _, srv := range rows {
		if !naiveDepleted(srv) && !naiveExpired(srv, now) {
			continue
		}
		if s.Status(srv.Id).Running {
			if err := s.Stop(srv.Id); err != nil {
				logger.Warningf("naive quota: stop %d: %v", srv.Id, err)
			} else {
				reason := "quota reached"
				if naiveExpired(srv, now) {
					reason = "expired"
				}
				logger.Infof("naive: server %d stopped (%s)", srv.Id, reason)
			}
		}
	}
}

// ResetTrafficBySchedule zeroes Up/Down for every server whose TrafficReset
// matches period (day|week|month) and stamps LastTrafficResetTime. Called by
// the periodic reset cron jobs. Returns the number of servers reset.
func (s *NaiveService) ResetTrafficBySchedule(period string) (int, error) {
	var rows []*model.NaiveServer
	if err := database.GetDB().Where("traffic_reset = ?", period).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	count := 0
	err := submitTrafficWrite(func() error {
		db := database.GetDB()
		for _, srv := range rows {
			if err := db.Model(&model.NaiveServer{}).Where("id = ?", srv.Id).
				UpdateColumns(map[string]any{
					"up": 0, "down": 0, "last_traffic_reset_time": now,
				}).Error; err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return count, err
	}
	// Resume servers that were stopped on quota: now that the counter is zero,
	// an enabled, non-expired server should come back up.
	for _, srv := range rows {
		if srv.Enable && !naiveExpired(srv, now) && !s.Status(srv.Id).Running {
			if startErr := s.Start(srv.Id); startErr != nil {
				logger.Warningf("naive reset: resume %d: %v", srv.Id, startErr)
			}
		}
	}
	return count, err
}

func (s *NaiveService) Status(id int) NaiveStatus {
	s.mu.Lock()
	st := NaiveStatus{Id: id}
	p, ok := s.procs[id]
	if !ok || p.cmd == nil || p.cmd.Process == nil {
		s.mu.Unlock()
		return st
	}
	if err := p.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		delete(s.procs, id)
		s.mu.Unlock()
		return st
	}
	st.Running = true
	st.Pid = p.cmd.Process.Pid
	st.Since = p.started.Unix()
	st.LogPath = p.logPath
	port := p.port
	s.mu.Unlock()

	// TCP probe — verify the port is actually accepting connections
	if port > 0 {
		host := "127.0.0.1"
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			st.Listening = true
		}
	}
	// HTTPS-level probe — TLS handshake completes ⇒ caddy is actually serving.
	// We don't care about the response body; forward_proxy returns 4xx/5xx
	// on plain HEAD, which still proves the server is alive.
	if st.Listening {
		st.Responding = probeHTTPS(port)
	}
	return st
}

// probeHTTPS does a 1s HTTPS HEAD against 127.0.0.1:port skipping certificate
// verification (the cert is for the public domain, not localhost). Returns
// true if either the TLS handshake or an HTTP response came back.
func probeHTTPS(port int) bool {
	client := &http.Client{
		Timeout: 1 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/", port)
	req, _ := http.NewRequest(http.MethodHead, url, nil)
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		return true
	}
	return false
}

func (s *NaiveService) Start(id int) error {
	srv, err := s.Get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.procs[id]; ok && p.cmd != nil && p.cmd.Process != nil {
		if err := p.cmd.Process.Signal(syscall.Signal(0)); err == nil {
			return ErrNaiveAlreadyRunning
		}
		delete(s.procs, id)
	}

	bin, err := resolveNaiveBinary()
	if err != nil {
		return err
	}
	dir, err := ensureNaiveDir()
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(dir, fmt.Sprintf("naive-%d.caddyfile", id))
	if err := writeNaiveConfig(cfgPath, srv); err != nil {
		return err
	}
	logPath := filepath.Join(dir, fmt.Sprintf("naive-%d.log", id))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}

	args := []string{"run", "--config", cfgPath, "--adapter", "caddyfile"}
	if extra := strings.TrimSpace(srv.ExtraArgs); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// isolate caddy state per server so instances don't clobber each other's
	// ACME storage, locks, etc.
	dataDir := filepath.Join(dir, fmt.Sprintf("data-%d", id))
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("mkdir data dir: %w", err)
	}
	cmd.Env = append(os.Environ(),
		"XDG_DATA_HOME="+dataDir,
		"XDG_CONFIG_HOME="+dataDir,
	)

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start naive: %w", err)
	}
	s.procs[id] = &naiveProc{cmd: cmd, started: time.Now(), logPath: logPath, port: srv.Port}
	logger.Infof("naive: server %d started, pid=%d", id, cmd.Process.Pid)

	// reap so dead procs disappear from status. recover() guards against
	// a panic inside this goroutine (e.g. weird logger/mutex state on
	// shutdown) — the panel must not crash because one naive instance died
	// in an unexpected way.
	go func(id int, c *exec.Cmd, lf *os.File) {
		defer func() {
			if r := recover(); r != nil {
				logger.Warningf("naive reaper %d: panic %v", id, r)
			}
		}()
		_ = c.Wait()
		_ = lf.Close()
		s.mu.Lock()
		if p, ok := s.procs[id]; ok && p.cmd == c {
			delete(s.procs, id)
		}
		s.mu.Unlock()
		logger.Infof("naive: server %d exited", id)
	}(id, cmd, logFile)

	return nil
}

func (s *NaiveService) Stop(id int) error {
	s.mu.Lock()
	p, ok := s.procs[id]
	s.mu.Unlock()
	if !ok || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}

func (s *NaiveService) Restart(id int) error {
	_ = s.Stop(id)
	time.Sleep(200 * time.Millisecond)
	return s.Start(id)
}

// Log returns the last `tail` lines from the per-server log file. The default
// is 200 lines; the cap of 1000 guards against accidentally tailing the
// whole file if someone passes a huge value. The reader only reads the last
// 256 KiB of the file from disk, so it's cheap even when the log is large.
func (s *NaiveService) Log(id, tail int) (string, error) {
	if tail <= 0 {
		tail = 200
	}
	if tail > 1000 {
		tail = 1000
	}
	dir, err := ensureNaiveDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("naive-%d.log", id))
	return tailLogFile(path, tail)
}

func tailLogFile(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // no log yet — return empty, not an error
		}
		return "", err
	}
	defer f.Close()

	const maxRead int64 = 256 * 1024
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() == 0 {
		return "", nil
	}
	var start int64
	if info.Size() > maxRead {
		start = info.Size() - maxRead
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

func (s *NaiveService) Restore() {
	rows, err := s.List()
	if err != nil {
		logger.Warningf("naive restore: %v", err)
		return
	}
	var wg sync.WaitGroup
	for _, srv := range rows {
		if !srv.Enable {
			continue
		}
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Warningf("naive restore %d: panic %v", id, r)
				}
			}()
			if err := s.Start(id); err != nil {
				logger.Warningf("naive restore %d: %v", id, err)
			}
		}(srv.Id)
	}
	wg.Wait()
	s.syncTrafficCounters()
}

func validateNaive(srv *model.NaiveServer) error {
	if srv == nil {
		return ErrNaiveInvalidConfig
	}
	// raw mode: panel doesn't generate, user provides the whole Caddyfile
	if srv.UseRawConfig {
		if strings.TrimSpace(srv.RawConfig) == "" {
			return fmt.Errorf("%w: raw config is empty", ErrNaiveInvalidConfig)
		}
		return nil
	}
	if srv.Port <= 0 || srv.Port > 65535 {
		return fmt.Errorf("%w: bad port", ErrNaiveInvalidConfig)
	}
	if strings.TrimSpace(srv.Domain) == "" {
		return fmt.Errorf("%w: domain required", ErrNaiveInvalidConfig)
	}
	if strings.TrimSpace(srv.AuthUser) == "" || strings.TrimSpace(srv.AuthPass) == "" {
		return fmt.Errorf("%w: auth required", ErrNaiveInvalidConfig)
	}
	if bad := badAuthChar(srv.AuthUser); bad != "" {
		return fmt.Errorf("%w: auth user contains forbidden character %s", ErrNaiveInvalidConfig, bad)
	}
	if bad := badAuthChar(srv.AuthPass); bad != "" {
		return fmt.Errorf("%w: auth pass contains forbidden character %s", ErrNaiveInvalidConfig, bad)
	}
	if strings.ContainsRune(srv.AuthUser, ':') {
		return fmt.Errorf("%w: auth user cannot contain ':' (reserved as user/password separator in client URL)", ErrNaiveInvalidConfig)
	}
	if len(srv.AuthUser) > 64 || len(srv.AuthPass) > 128 {
		return fmt.Errorf("%w: auth user/pass too long (max 64 / 128)", ErrNaiveInvalidConfig)
	}
	if srv.UseACME {
		if strings.TrimSpace(srv.AcmeEmail) == "" {
			return fmt.Errorf("%w: acme email required", ErrNaiveInvalidConfig)
		}
	} else if strings.TrimSpace(srv.CertFile) == "" || strings.TrimSpace(srv.KeyFile) == "" {
		return fmt.Errorf("%w: cert/key required (or enable ACME)", ErrNaiveInvalidConfig)
	}
	// extra users: same charset/length rules; usernames must be unique across
	// the primary user and all extras (duplicate basic_auth users are ambiguous).
	seen := map[string]bool{srv.AuthUser: true}
	for _, u := range srv.Users {
		if u == nil || strings.TrimSpace(u.Username) == "" {
			continue
		}
		if strings.TrimSpace(u.Password) == "" {
			return fmt.Errorf("%w: user %q requires a password", ErrNaiveInvalidConfig, u.Username)
		}
		if err := validateAuthPair(u.Username, u.Password); err != nil {
			return err
		}
		if seen[u.Username] {
			return fmt.Errorf("%w: duplicate user %q", ErrNaiveInvalidConfig, u.Username)
		}
		seen[u.Username] = true
	}
	return nil
}

// validateAuthPair applies the basic_auth charset / length / ':' rules to one
// username+password pair. Shared by the primary credential and extra users.
func validateAuthPair(user, pass string) error {
	if bad := badAuthChar(user); bad != "" {
		return fmt.Errorf("%w: auth user contains forbidden character %s", ErrNaiveInvalidConfig, bad)
	}
	if bad := badAuthChar(pass); bad != "" {
		return fmt.Errorf("%w: auth pass contains forbidden character %s", ErrNaiveInvalidConfig, bad)
	}
	if strings.ContainsRune(user, ':') {
		return fmt.Errorf("%w: auth user cannot contain ':' (reserved as user/password separator in client URL)", ErrNaiveInvalidConfig)
	}
	if len(user) > 64 || len(pass) > 128 {
		return fmt.Errorf("%w: auth user/pass too long (max 64 / 128)", ErrNaiveInvalidConfig)
	}
	return nil
}

// RenderCaddyfile returns the Caddyfile text the panel would produce for srv.
// Used by the UI to preview the generated config or to seed the raw-mode editor.
func RenderCaddyfile(srv *model.NaiveServer) string {
	if srv == nil {
		return ""
	}
	if srv.UseRawConfig {
		return srv.RawConfig
	}
	return renderNaiveCaddyfile(srv)
}

// ValidateCaddyfile runs `caddy adapt` against the given text to verify syntax.
// Requires a working caddy binary; if none is installed, returns a clear error.
func ValidateCaddyfile(ctx context.Context, text string) error {
	bin, _ := findCaddy()
	if bin == "" {
		return ErrNaiveBinaryMissing
	}
	tmp, err := os.CreateTemp("", "naive-validate-*.caddyfile")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Close()

	cmd := exec.CommandContext(ctx, bin, "adapt", "--config", tmp.Name(), "--adapter", "caddyfile")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

// writeNaiveConfig emits a Caddyfile for the klzgrad/forwardproxy fork of Caddy
// see https://github.com/klzgrad/naiveproxy server setup
func writeNaiveConfig(path string, srv *model.NaiveServer) error {
	if srv.UseRawConfig {
		return os.WriteFile(path, []byte(srv.RawConfig), 0o600)
	}
	return os.WriteFile(path, []byte(renderNaiveCaddyfile(srv)), 0o600)
}

func renderNaiveCaddyfile(srv *model.NaiveServer) string {
	listen := strings.TrimSpace(srv.Listen)
	// 0.0.0.0 / :: / empty all mean "bind everywhere" — Caddy uses bare :port for that.
	bindAll := listen == "" || listen == "0.0.0.0" || listen == "::"

	level := strings.ToUpper(strings.TrimSpace(firstNonEmpty(srv.LogLevel, "WARN")))
	if level == "WARNING" {
		level = "WARN" // back-compat for old rows
	}

	var b strings.Builder
	// admin off so multiple naive instances don't fight over :2019
	b.WriteString("{\n")
	b.WriteString("\tadmin off\n")
	fmt.Fprintf(&b, "\tlog {\n\t\tlevel %s\n\t}\n", level)
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, ":%d, %s {\n", srv.Port, srv.Domain)
	if !bindAll {
		fmt.Fprintf(&b, "\tbind %s\n", listen)
	}
	if srv.UseACME {
		// ACME: Caddy will obtain certs via Let's Encrypt
		fmt.Fprintf(&b, "\ttls %s\n", srv.AcmeEmail)
	} else {
		fmt.Fprintf(&b, "\ttls %s %s\n", srv.CertFile, srv.KeyFile)
	}
	b.WriteString("\troute {\n")
	b.WriteString("\t\tforward_proxy {\n")
	fmt.Fprintf(&b, "\t\t\tbasic_auth %s %s\n", srv.AuthUser, srv.AuthPass)
	// extra users: one basic_auth line each (forward_proxy allows many)
	for _, u := range srv.Users {
		if u != nil && u.Enable && strings.TrimSpace(u.Username) != "" {
			fmt.Fprintf(&b, "\t\t\tbasic_auth %s %s\n", u.Username, u.Password)
		}
	}
	b.WriteString("\t\t\thide_ip\n")
	b.WriteString("\t\t\thide_via\n")
	b.WriteString("\t\t\tprobe_resistance\n")
	// NOTE: no explicit `padding` directive. The klzgrad/forwardproxy fork
	// pads HTTP/2 traffic by default and rejects a bare `padding` token
	// (`caddy adapt` fails: "wrong argument count ... after 'padding'"),
	// which would stop the server from starting. srv.Padding is kept for
	// API/DB back-compat but no longer emitted.
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")

	return b.String()
}

// badAuthChar returns a human-readable description of the first forbidden
// character in s, or "" if all characters are safe. Forbidden:
// whitespace (Caddyfile token separator), control chars, and Caddyfile
// metacharacters ", \, #.
func badAuthChar(s string) string {
	for _, r := range s {
		switch {
		case r == ' ':
			return "space"
		case r == '\t':
			return "tab"
		case r == '\n' || r == '\r':
			return "newline"
		case r == '"':
			return `"`
		case r == '\\':
			return `\`
		case r == '#':
			return "#"
		case r < 0x20 || r == 0x7F:
			return fmt.Sprintf("control U+%04X", r)
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func resolveNaiveBinary() (string, error) {
	p, _ := findCaddy()
	if p == "" {
		return "", ErrNaiveBinaryMissing
	}
	return p, nil
}

func ensureNaiveDir() (string, error) {
	dir := filepath.Join(config.GetBinFolderPath(), "naive")
	return dir, os.MkdirAll(dir, 0o700)
}
