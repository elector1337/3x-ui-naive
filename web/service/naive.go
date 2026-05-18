// Package service — NaiveProxy server management.
//
// NaiveProxy (https://github.com/klzgrad/naiveproxy) is shipped as a standalone
// binary built from a patched Chromium network stack. It is not an Xray protocol,
// so we manage it as an external process: generate a JSON config, fork the
// binary with `--config=<path>`, track the PID, capture stdout/stderr to a log
// file. Lifecycle is intentionally simple — no supervision tree, the panel
// restarts servers on panel boot via Restore().
package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// Errors returned by NaiveService.
var (
	ErrNaiveNotFound       = errors.New("naive server not found")
	ErrNaiveBinaryMissing  = errors.New("naive binary not found (set NAIVE_BIN or place 'naive' in PATH)")
	ErrNaiveAlreadyRunning = errors.New("naive server already running")
	ErrNaiveInvalidConfig  = errors.New("invalid naive server config")
)

// NaiveStatus describes the current runtime state of a managed naive process.
type NaiveStatus struct {
	Id      int    `json:"id"`
	Running bool   `json:"running"`
	Pid     int    `json:"pid,omitempty"`
	Since   int64  `json:"since,omitempty"`
	LogPath string `json:"logPath,omitempty"`
}

type naiveProc struct {
	cmd     *exec.Cmd
	started time.Time
	logPath string
}

// NaiveService owns the table of NaiveServer rows and the in-memory map of
// running processes. It is safe to use from concurrent handlers — all state
// changes go through the mutex.
type NaiveService struct {
	mu    sync.Mutex
	procs map[int]*naiveProc
}

func newNaiveService() *NaiveService {
	return &NaiveService{procs: make(map[int]*naiveProc)}
}

// naiveSvc is a package-level singleton so controllers can find it without
// threading a pointer through the whole server wiring.
var naiveSvc = newNaiveService()

// GetNaiveService returns the panel-wide NaiveService instance.
func GetNaiveService() *NaiveService { return naiveSvc }

// --- CRUD ---

// List returns all configured naive servers ordered by id.
func (s *NaiveService) List() ([]*model.NaiveServer, error) {
	db := database.GetDB()
	var rows []*model.NaiveServer
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Get fetches a single naive server by id.
func (s *NaiveService) Get(id int) (*model.NaiveServer, error) {
	db := database.GetDB()
	row := &model.NaiveServer{}
	if err := db.First(row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNaiveNotFound
		}
		return nil, err
	}
	return row, nil
}

// Add validates and persists a new naive server.
func (s *NaiveService) Add(srv *model.NaiveServer) error {
	if err := validateNaive(srv); err != nil {
		return err
	}
	return database.GetDB().Create(srv).Error
}

// Update persists changes to an existing naive server. The caller is responsible
// for restarting the running process if it needs to pick up the new config.
func (s *NaiveService) Update(srv *model.NaiveServer) error {
	if err := validateNaive(srv); err != nil {
		return err
	}
	return database.GetDB().Save(srv).Error
}

// Delete stops the process (if running) and removes the row.
func (s *NaiveService) Delete(id int) error {
	_ = s.Stop(id) // best-effort: deleting a dead row should still succeed
	return database.GetDB().Delete(&model.NaiveServer{}, id).Error
}

// --- runtime ---

// Status reports whether the naive server with the given id is currently running.
func (s *NaiveService) Status(id int) NaiveStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := NaiveStatus{Id: id}
	if p, ok := s.procs[id]; ok && p.cmd != nil && p.cmd.Process != nil {
		// process may have exited without us noticing — probe with signal 0
		if err := p.cmd.Process.Signal(syscall.Signal(0)); err == nil {
			st.Running = true
			st.Pid = p.cmd.Process.Pid
			st.Since = p.started.Unix()
			st.LogPath = p.logPath
		} else {
			delete(s.procs, id)
		}
	}
	return st
}

// Start launches the naive binary for the given server.
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
	configDir, err := ensureNaiveDir()
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(configDir, fmt.Sprintf("naive-%d.json", id))
	if err := writeNaiveConfig(cfgPath, srv); err != nil {
		return err
	}
	logPath := filepath.Join(configDir, fmt.Sprintf("naive-%d.log", id))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open naive log: %w", err)
	}

	args := []string{"--config=" + cfgPath}
	if extra := strings.TrimSpace(srv.ExtraArgs); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start naive: %w", err)
	}

	s.procs[id] = &naiveProc{cmd: cmd, started: time.Now(), logPath: logPath}
	logger.Infof("naive: server %d started, pid=%d", id, cmd.Process.Pid)

	// reap on exit so a crashed process is reflected in Status()
	go func(id int, c *exec.Cmd, lf *os.File) {
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

// Stop terminates the running process for the given id (no-op if not running).
func (s *NaiveService) Stop(id int) error {
	s.mu.Lock()
	p, ok := s.procs[id]
	s.mu.Unlock()
	if !ok || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// SIGTERM may fail if the process is already dead — try SIGKILL as a fallback
		_ = p.cmd.Process.Kill()
	}
	return nil
}

// Restart is a convenience wrapper around Stop+Start.
func (s *NaiveService) Restart(id int) error {
	_ = s.Stop(id)
	// brief pause so the OS releases the listening socket
	time.Sleep(200 * time.Millisecond)
	return s.Start(id)
}

// Restore launches every server whose Enable=true. Call this once on panel boot.
func (s *NaiveService) Restore() {
	rows, err := s.List()
	if err != nil {
		logger.Warningf("naive: restore: list failed: %v", err)
		return
	}
	for _, srv := range rows {
		if !srv.Enable {
			continue
		}
		if err := s.Start(srv.Id); err != nil {
			logger.Warningf("naive: restore server %d: %v", srv.Id, err)
		}
	}
}

// --- helpers ---

func validateNaive(srv *model.NaiveServer) error {
	if srv == nil {
		return ErrNaiveInvalidConfig
	}
	if srv.Port <= 0 || srv.Port > 65535 {
		return fmt.Errorf("%w: port out of range", ErrNaiveInvalidConfig)
	}
	if strings.TrimSpace(srv.Domain) == "" {
		return fmt.Errorf("%w: domain is required", ErrNaiveInvalidConfig)
	}
	if strings.TrimSpace(srv.AuthUser) == "" || strings.TrimSpace(srv.AuthPass) == "" {
		return fmt.Errorf("%w: auth user/pass required", ErrNaiveInvalidConfig)
	}
	if strings.TrimSpace(srv.CertFile) == "" || strings.TrimSpace(srv.KeyFile) == "" {
		return fmt.Errorf("%w: cert/key paths required", ErrNaiveInvalidConfig)
	}
	return nil
}

// writeNaiveConfig renders the JSON config consumed by the naive binary.
// Schema reference: https://github.com/klzgrad/naiveproxy#server-setup
func writeNaiveConfig(path string, srv *model.NaiveServer) error {
	listen := srv.Listen
	if listen == "" {
		listen = "0.0.0.0"
	}
	cfg := map[string]any{
		"listen":    fmt.Sprintf("https://%s@%s:%d", buildAuth(srv), listen, srv.Port),
		"cert":      srv.CertFile,
		"key":       srv.KeyFile,
		"log":       "",
		"log_level": strings.ToUpper(strings.TrimSpace(firstNonEmpty(srv.LogLevel, "WARNING"))),
	}
	if srv.Padding {
		cfg["padding"] = true
	}
	if srv.Domain != "" {
		cfg["host"] = srv.Domain
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal naive config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write naive config: %w", err)
	}
	return nil
}

func buildAuth(srv *model.NaiveServer) string {
	// naive expects "user:pass" inline in the listen URL
	return fmt.Sprintf("%s:%s", srv.AuthUser, srv.AuthPass)
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
	if env := strings.TrimSpace(os.Getenv("NAIVE_BIN")); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
		return "", fmt.Errorf("%w: NAIVE_BIN=%q", ErrNaiveBinaryMissing, env)
	}
	if p, err := exec.LookPath("naive"); err == nil {
		return p, nil
	}
	return "", ErrNaiveBinaryMissing
}

func ensureNaiveDir() (string, error) {
	dir := filepath.Join(config.GetBinFolderPath(), "naive")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir naive dir: %w", err)
	}
	return dir, nil
}
