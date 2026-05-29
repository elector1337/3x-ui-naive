package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/config"
)

// CaddyInstaller builds caddy-forwardproxy via xcaddy and drops it into <bin>/caddy.
// We can't ship a prebuilt binary because klzgrad doesn't publish caddy+forwardproxy,
// and bundling per-arch builds is too much; xcaddy is the standard way.
type CaddyInstaller struct{}

func NewCaddyInstaller() *CaddyInstaller { return &CaddyInstaller{} }

type CaddyInstallStatus struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Source    string `json:"source,omitempty"` // "panel", "env", "path"
	Version   string `json:"version,omitempty"`
	GoPresent bool   `json:"goPresent"`
}

// PanelCaddyPath returns the path where the panel-managed caddy binary lives.
func PanelCaddyPath() string {
	return filepath.Join(config.GetBinFolderPath(), "caddy")
}

// Status reports whether a caddy binary is currently usable and where it came from.
func (c *CaddyInstaller) Status() CaddyInstallStatus {
	st := CaddyInstallStatus{}
	st.GoPresent = goAvailable()

	p, source := findCaddy()
	if p == "" {
		return st
	}
	st.Installed = true
	st.Path = p
	st.Source = source
	st.Version = caddyVersion(p)
	return st
}

// Install runs xcaddy to build a caddy binary with the forwardproxy plugin.
// The result is written to <bin>/caddy. Output is streamed into the returned channel.
func (c *CaddyInstaller) Install(ctx context.Context, out chan<- string) error {
	defer close(out)

	if !goAvailable() {
		//nolint:staticcheck // ST1005: "Go" is a proper noun and must stay capitalized
		return errors.New("Go toolchain not found in PATH — install Go 1.22+ first (https://go.dev/dl/)")
	}

	binDir := config.GetBinFolderPath()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("mkdir bin: %w", err)
	}
	outPath := PanelCaddyPath()
	gobin := filepath.Join(binDir, "gobin")
	if err := os.MkdirAll(gobin, 0o755); err != nil {
		return fmt.Errorf("mkdir gobin: %w", err)
	}

	emit := func(s string) {
		select {
		case out <- s:
		case <-ctx.Done():
		}
	}

	// step 1: install xcaddy
	emit("==> installing xcaddy")
	xcaddyPath, err := ensureXCaddy(ctx, gobin, emit)
	if err != nil {
		return err
	}
	emit("xcaddy: " + xcaddyPath)

	// step 2: build caddy with forwardproxy plugin
	emit("==> building caddy with forward_proxy plugin (this can take a couple of minutes)")
	buildCmd := exec.CommandContext(ctx, xcaddyPath,
		"build",
		"--with", "github.com/caddyserver/forwardproxy@caddy2=github.com/klzgrad/forwardproxy@naive",
		"--output", outPath,
	)
	buildCmd.Env = append(os.Environ(), "GOPATH="+filepath.Dir(gobin), "GOBIN="+gobin)
	if err := runWithStream(buildCmd, emit); err != nil {
		return fmt.Errorf("xcaddy build failed: %w", err)
	}

	if err := os.Chmod(outPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	emit("==> done: " + outPath)
	emit("version: " + caddyVersion(outPath))
	return nil
}

// findCaddy returns the path and source of a usable caddy binary, or "" if none found.
func findCaddy() (path, source string) {
	if env := strings.TrimSpace(os.Getenv("CADDY_BIN")); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, "env"
		}
	}
	panelBin := PanelCaddyPath()
	if _, err := os.Stat(panelBin); err == nil {
		return panelBin, "panel"
	}
	if p, err := exec.LookPath("caddy"); err == nil {
		return p, "path"
	}
	return "", ""
}

func caddyVersion(path string) string {
	cmd := exec.Command(path, "version")
	cmd.Env = append(os.Environ(), "HOME="+os.TempDir())
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	// caddy outputs "v2.x.x ..." — keep first token
	if i := strings.IndexByte(v, ' '); i > 0 {
		v = v[:i]
	}
	return v
}

func goAvailable() bool {
	_, err := exec.LookPath("go")
	return err == nil
}

func ensureXCaddy(ctx context.Context, gobin string, emit func(string)) (string, error) {
	target := filepath.Join(gobin, "xcaddy")
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	cmd := exec.CommandContext(ctx, "go", "install", "github.com/caddyserver/xcaddy/cmd/xcaddy@latest")
	cmd.Env = append(os.Environ(), "GOBIN="+gobin, "GOPATH="+filepath.Dir(gobin))
	if err := runWithStream(cmd, emit); err != nil {
		return "", fmt.Errorf("go install xcaddy: %w", err)
	}
	if _, err := os.Stat(target); err != nil {
		return "", fmt.Errorf("xcaddy not found after install at %s", target)
	}
	return target, nil
}

func runWithStream(cmd *exec.Cmd, emit func(string)) error {
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Start(); err != nil {
		_ = w.Close()
		_ = r.Close()
		return err
	}
	_ = w.Close()

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				for _, line := range strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n") {
					emit(line)
				}
			}
			if err != nil {
				return
			}
		}
	}()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		<-streamDone
		return err
	case <-time.After(10 * time.Minute):
		_ = cmd.Process.Kill()
		return errors.New("install timed out after 10 minutes")
	}
}
