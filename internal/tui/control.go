package tui

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charlesolinsky/local-llm/internal/config"
)

type controller struct {
	store   *config.Store
	envPath string

	mu    sync.Mutex
	child *exec.Cmd
}

func newController(store *config.Store, envPath string) *controller {
	return &controller{store: store, envPath: envPath}
}

func (c *controller) snap() config.Config {
	return c.store.Snapshot()
}

func (c *controller) listenHostPort() string {
	addr := c.snap().Listen
	if strings.HasPrefix(addr, "0.0.0.0") {
		return "127.0.0.1" + strings.TrimPrefix(addr, "0.0.0.0")
	}
	if strings.HasPrefix(addr, "[::]") {
		return "127.0.0.1" + strings.TrimPrefix(addr, "[::]")
	}
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	return addr
}

func (c *controller) gatewayUp() bool {
	conn, err := net.DialTimeout("tcp", c.listenHostPort(), 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (c *controller) ollamaUp() bool {
	base := strings.TrimPrefix(strings.TrimPrefix(c.snap().OllamaBase, "http://"), "https://")
	base = strings.TrimRight(base, "/")
	conn, err := net.DialTimeout("tcp", base, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (c *controller) owned() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.child != nil && c.child.Process != nil
}

func (c *controller) start() error {
	if c.gatewayUp() {
		return fmt.Errorf("already listening on %s", c.snap().Listen)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"serve"}
	if c.store.Path() != "" {
		args = append(args, "--config", c.store.Path())
	}
	if c.envPath != "" {
		args = append(args, "--env-file", c.envPath)
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = os.Environ()
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}

	c.mu.Lock()
	c.child = cmd
	c.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c.gatewayUp() {
			go func() { _ = cmd.Wait() }()
			return nil
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			c.mu.Lock()
			c.child = nil
			c.mu.Unlock()
			return fmt.Errorf("serve exited before listening")
		}
		time.Sleep(80 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	c.mu.Lock()
	c.child = nil
	c.mu.Unlock()
	return fmt.Errorf("serve started but did not bind %s", c.snap().Listen)
}

func (c *controller) stop() error {
	c.mu.Lock()
	child := c.child
	c.mu.Unlock()

	if child != nil && child.Process != nil {
		_ = child.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_, _ = child.Process.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			_ = child.Process.Kill()
		}
		c.mu.Lock()
		c.child = nil
		c.mu.Unlock()
		return nil
	}

	pid, name, err := listenerPID(listenPort(c.snap().Listen))
	if err != nil {
		return err
	}
	if pid == 0 {
		return fmt.Errorf("nothing listening on %s", c.snap().Listen)
	}
	if name != "" && !strings.Contains(strings.ToLower(name), "local-llm") {
		return fmt.Errorf("port is held by %s (pid %d), not local-llm", name, pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if !c.gatewayUp() {
			return nil
		}
		time.Sleep(80 * time.Millisecond)
	}
	_ = proc.Kill()
	return nil
}

func listenerPID(port string) (int, string, error) {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN").CombinedOutput()
	if err != nil {
		// lsof exits 1 when nothing matches.
		if len(out) == 0 {
			return 0, "", nil
		}
		return 0, "", fmt.Errorf("lsof: %s", strings.TrimSpace(string(out)))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, "", nil
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 2 {
		return 0, "", nil
	}
	pid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fields[0], nil
	}
	return pid, fields[0], nil
}
