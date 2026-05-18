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

var (
	ErrNaiveNotFound       = errors.New("naive server not found")
	ErrNaiveBinaryMissing  = errors.New("naive binary not found")
	ErrNaiveAlreadyRunning = errors.New("naive server already running")
	ErrNaiveInvalidConfig  = errors.New("invalid naive server config")
)

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

type NaiveService struct {
	mu    sync.Mutex
	procs map[int]*naiveProc
}

var naiveSvc = &NaiveService{procs: make(map[int]*naiveProc)}

func GetNaiveService() *NaiveService { return naiveSvc }

func (s *NaiveService) List() ([]*model.NaiveServer, error) {
	var rows []*model.NaiveServer
	err := database.GetDB().Order("id asc").Find(&rows).Error
	return rows, err
}

func (s *NaiveService) Get(id int) (*model.NaiveServer, error) {
	row := &model.NaiveServer{}
	if err := database.GetDB().First(row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNaiveNotFound
		}
		return nil, err
	}
	return row, nil
}

func (s *NaiveService) Add(srv *model.NaiveServer) error {
	if err := validateNaive(srv); err != nil {
		return err
	}
	return database.GetDB().Create(srv).Error
}

func (s *NaiveService) Update(srv *model.NaiveServer) error {
	if err := validateNaive(srv); err != nil {
		return err
	}
	return database.GetDB().Save(srv).Error
}

func (s *NaiveService) Delete(id int) error {
	_ = s.Stop(id)
	return database.GetDB().Delete(&model.NaiveServer{}, id).Error
}

func (s *NaiveService) Status(id int) NaiveStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := NaiveStatus{Id: id}
	p, ok := s.procs[id]
	if !ok || p.cmd == nil || p.cmd.Process == nil {
		return st
	}
	// signal 0 probes if the pid is still alive
	if err := p.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		delete(s.procs, id)
		return st
	}
	st.Running = true
	st.Pid = p.cmd.Process.Pid
	st.Since = p.started.Unix()
	st.LogPath = p.logPath
	return st
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
	cfgPath := filepath.Join(dir, fmt.Sprintf("naive-%d.json", id))
	if err := writeNaiveConfig(cfgPath, srv); err != nil {
		return err
	}
	logPath := filepath.Join(dir, fmt.Sprintf("naive-%d.log", id))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
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

	// reap so dead procs disappear from status
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

func (s *NaiveService) Restore() {
	rows, err := s.List()
	if err != nil {
		logger.Warningf("naive restore: %v", err)
		return
	}
	for _, srv := range rows {
		if !srv.Enable {
			continue
		}
		if err := s.Start(srv.Id); err != nil {
			logger.Warningf("naive restore %d: %v", srv.Id, err)
		}
	}
}

func validateNaive(srv *model.NaiveServer) error {
	if srv == nil {
		return ErrNaiveInvalidConfig
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
	if strings.TrimSpace(srv.CertFile) == "" || strings.TrimSpace(srv.KeyFile) == "" {
		return fmt.Errorf("%w: cert/key required", ErrNaiveInvalidConfig)
	}
	return nil
}

func writeNaiveConfig(path string, srv *model.NaiveServer) error {
	listen := srv.Listen
	if listen == "" {
		listen = "0.0.0.0"
	}
	cfg := map[string]any{
		"listen":    fmt.Sprintf("https://%s:%s@%s:%d", srv.AuthUser, srv.AuthPass, listen, srv.Port),
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
		return err
	}
	return os.WriteFile(path, data, 0o600)
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
	return dir, os.MkdirAll(dir, 0o700)
}
