package appruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Spec struct {
	Node        string
	Directory   string
	Arguments   []string
	Environment map[string]string
	Log         io.Writer
}
type Process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
	kill func() error
	once sync.Once
}

// Environment is a fresh allowlist, never the Backend's database/Fleet/token
// environment. App-scoped credentials must be supplied explicitly by its owner.
func Environment(node string, values map[string]string) []string {
	env := map[string]string{}
	for _, k := range []string{"SYSTEMROOT", "WINDIR", "COMSPEC", "TEMP", "TMP", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "HOME", "LANG", "PATH", "PATHEXT"} {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	env["PATH"] = filepath.Dir(node) + string(os.PathListSeparator) + env["PATH"]
	env["NODE_ENV"] = "production"
	for k, v := range values {
		env[strings.ToUpper(k)] = v
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}
func Start(s Spec) (*Process, error) {
	if !filepath.IsAbs(s.Node) || !filepath.IsAbs(s.Directory) {
		return nil, errors.New("native App executable and directory must be absolute")
	}
	info, err := os.Stat(s.Node)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("bundled Node executable unavailable")
	}
	cmd := exec.Command(s.Node, s.Arguments...)
	cmd.Dir = s.Directory
	cmd.Env = Environment(s.Node, s.Environment)
	cmd.Stdout = s.Log
	cmd.Stderr = s.Log
	kill, err := startOwned(cmd)
	if err != nil {
		return nil, fmt.Errorf("start native App process: %w", err)
	}
	p := &Process{cmd: cmd, kill: kill, done: make(chan struct{})}
	go func() { p.err = cmd.Wait(); _ = kill(); close(p.done) }()
	return p, nil
}
func (p *Process) Done() <-chan struct{} { return p.done }
func (p *Process) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return p.err
	}
}
func (p *Process) Stop() error {
	var err error
	p.once.Do(func() { err = p.kill() })
	select {
	case <-p.done:
		return err
	case <-time.After(10 * time.Second):
		return errors.New("native App process stop timed out")
	}
}

// Port is never advertised to the user; public access uses /apps/<id>/.
func AllocatePort() (int, error) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	err = l.Close()
	return port, err
}
func (p *Process) WaitHealthy(ctx context.Context, port int, path string) error {
	if port < 1 || port > 65535 || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return errors.New("invalid native App health target")
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("native App health check: %w", ctx.Err())
		case <-p.done:
			return errors.New("native App exited before becoming healthy")
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
			if err != nil {
				return err
			}
			response, err := client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
				response.Body.Close()
				if response.StatusCode >= 200 && response.StatusCode < 400 {
					return nil
				}
			}
		}
	}
}

// Run executes an install/build phase in the same owned process tree as runtime.
func Run(ctx context.Context, s Spec) error {
	p, err := Start(s)
	if err != nil {
		return err
	}
	defer p.Stop()
	return p.Wait(ctx)
}
