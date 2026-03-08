package guardian

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lovitus/processgod-mac/internal/config"
	"github.com/lovitus/processgod-mac/internal/cron"
	"github.com/lovitus/processgod-mac/internal/logbuf"
)

const (
	defaultTickInterval = 3 * time.Second
	defaultLogLines     = 4000
)

// Status describes runtime status of one guarded item.
type Status struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ExecPath  string    `json:"execPath"`
	Running   bool      `json:"running"`
	PID       int       `json:"pid"`
	LastStart time.Time `json:"lastStart,omitempty"`
	LastExit  time.Time `json:"lastExit,omitempty"`
	LastError string    `json:"lastError,omitempty"`
}

// Manager owns guarded process lifecycle.
type Manager struct {
	mu           sync.Mutex
	procs        map[string]*managed
	tickInterval time.Duration
	logger       *log.Logger
}

type managed struct {
	item        config.Item
	cronSched   *cron.Schedule
	ring        *logbuf.Ring
	cmd         *exec.Cmd
	running     bool
	pid         int
	startedOnce bool
	lastCronKey string
	lastStart   time.Time
	lastExit    time.Time
	lastError   string
}

func New(logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.New(os.Stdout, "[guardian] ", log.LstdFlags)
	}
	return &Manager{
		procs:        make(map[string]*managed),
		tickInterval: defaultTickInterval,
		logger:       logger,
	}
}

func (m *Manager) Apply(cfg config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	next := make(map[string]config.Item)
	for _, it := range cfg.Items {
		if !it.Started {
			continue
		}
		it.Normalize()
		next[it.ID] = it
	}

	for id, p := range m.procs {
		if _, ok := next[id]; !ok {
			m.stopLocked(p)
			delete(m.procs, id)
		}
	}

	for id, it := range next {
		existing, ok := m.procs[id]
		if !ok {
			m.procs[id] = newManaged(it)
			continue
		}
		if !sameRuntimeConfig(existing.item, it) {
			m.stopLocked(existing)
			existing.item = it
			existing.startedOnce = false
			existing.lastCronKey = ""
			existing.lastError = ""
			existing.cronSched = parseCron(it.CronExpression)
		}
	}

	return nil
}

func (m *Manager) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(m.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			m.shutdown()
			return
		case now := <-ticker.C:
			m.tick(now)
		}
	}
}

func (m *Manager) ReloadFrom(path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil {
		return err
	}
	return m.Apply(cfg)
}

func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Status, 0, len(m.procs))
	for _, p := range m.procs {
		out = append(out, Status{
			ID:        p.item.ID,
			Name:      p.item.ProcessName,
			ExecPath:  p.item.ExecPath,
			Running:   p.running,
			PID:       p.pid,
			LastStart: p.lastStart,
			LastExit:  p.lastExit,
			LastError: p.lastError,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Manager) Logs(id string, lines int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.procs[id]
	if !ok {
		return "", fmt.Errorf("process id %q not found", id)
	}
	if p.ring == nil {
		return "", nil
	}
	return p.ring.Last(lines), nil
}

func (m *Manager) tick(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.procs {
		m.handleCronLocked(p, now)

		if p.running {
			continue
		}
		if p.item.OnlyOpenOnce && p.startedOnce {
			continue
		}
		if p.item.CronExpression != "" && !p.item.StopBeforeCronExec {
			continue
		}

		m.startLocked(p, now)
	}
}

func (m *Manager) handleCronLocked(p *managed, now time.Time) {
	if p.cronSched == nil {
		return
	}
	if !p.cronSched.Matches(now) {
		return
	}
	key := now.Format("200601021504")
	if key == p.lastCronKey {
		return
	}
	p.lastCronKey = key

	if p.item.StopBeforeCronExec {
		if p.running {
			m.stopLocked(p)
		}
		m.startLocked(p, now)
		return
	}
	if !p.running {
		m.startLocked(p, now)
	}
}

func (m *Manager) startLocked(p *managed, now time.Time) {
	if p.item.ExecPath == "" {
		p.lastError = "execPath is empty"
		return
	}
	if _, err := os.Stat(p.item.ExecPath); err != nil {
		p.lastError = fmt.Sprintf("exec path not available: %v", err)
		return
	}

	cmd := exec.Command(p.item.ExecPath, p.item.Args...)
	if p.item.WorkingDir != "" {
		cmd.Dir = p.item.WorkingDir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(p.item.Env) > 0 {
		env := os.Environ()
		for k, v := range p.item.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.lastError = fmt.Sprintf("stdout pipe: %v", err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.lastError = fmt.Sprintf("stderr pipe: %v", err)
		return
	}

	if err := cmd.Start(); err != nil {
		p.lastError = fmt.Sprintf("start process: %v", err)
		return
	}

	p.cmd = cmd
	p.running = true
	p.pid = cmd.Process.Pid
	p.lastStart = now
	p.lastError = ""
	p.startedOnce = true
	if p.ring == nil {
		p.ring = logbuf.New(defaultLogLines)
	}

	m.logger.Printf("started %s (%d)", p.item.ID, p.pid)

	go m.consumeOutput(p.item.ID, p.ring, stdout)
	go m.consumeOutput(p.item.ID, p.ring, stderr)
	go m.waitForExit(p, cmd)
}

func (m *Manager) waitForExit(p *managed, cmd *exec.Cmd) {
	err := cmd.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	if p.cmd != cmd {
		return
	}

	p.running = false
	p.pid = 0
	p.cmd = nil
	p.lastExit = time.Now()
	if err != nil {
		p.lastError = err.Error()
		m.logger.Printf("process %s exited with error: %v", p.item.ID, err)
	} else {
		m.logger.Printf("process %s exited", p.item.ID)
	}
}

func (m *Manager) consumeOutput(id string, ring *logbuf.Ring, reader interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		ring.Add(scanner.Text())
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
		ring.Add("[output-reader-error] " + err.Error())
	}
}

func (m *Manager) stopLocked(p *managed) {
	if p == nil || !p.running || p.cmd == nil || p.cmd.Process == nil {
		p.running = false
		p.pid = 0
		p.cmd = nil
		return
	}

	pid := p.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(800 * time.Millisecond)
	_ = syscall.Kill(-pid, syscall.SIGKILL)

	p.running = false
	p.pid = 0
	p.cmd = nil
	p.lastExit = time.Now()
	m.logger.Printf("stopped %s (%d)", p.item.ID, pid)
}

func (m *Manager) shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.procs {
		m.stopLocked(p)
	}
}

func newManaged(it config.Item) *managed {
	it.Normalize()
	return &managed{
		item:      it,
		cronSched: parseCron(it.CronExpression),
		ring:      logbuf.New(defaultLogLines),
	}
}

func parseCron(expr string) *cron.Schedule {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	s, err := cron.Parse(expr)
	if err != nil {
		return nil
	}
	return s
}

func sameRuntimeConfig(a, b config.Item) bool {
	if a.ID != b.ID || a.ProcessName != b.ProcessName || a.ExecPath != b.ExecPath || a.WorkingDir != b.WorkingDir {
		return false
	}
	if a.StartupParams != b.StartupParams || a.OnlyOpenOnce != b.OnlyOpenOnce || a.NoWindow != b.NoWindow {
		return false
	}
	if a.CronExpression != b.CronExpression || a.StopBeforeCronExec != b.StopBeforeCronExec {
		return false
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	if len(a.Env) != len(b.Env) {
		return false
	}
	for k, v := range a.Env {
		if b.Env[k] != v {
			return false
		}
	}
	return true
}
