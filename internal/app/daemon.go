package app

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/lovitus/processgod-mac/internal/config"
	"github.com/lovitus/processgod-mac/internal/guardian"
	"github.com/lovitus/processgod-mac/internal/ipc"
	"github.com/lovitus/processgod-mac/internal/service"
)

type Daemon struct {
	configPath  string
	controlAddr string
	manager     *guardian.Manager
	logger      *log.Logger
	level       string
	levelHint   string
	stopOnce    sync.Once
	stopFunc    func()
}

func NewDaemon(configPath, controlAddr string) *Daemon {
	logger := log.New(os.Stdout, "[processgod] ", log.LstdFlags)
	level, hint := detectLevel()
	return &Daemon{
		configPath:  configPath,
		controlAddr: controlAddr,
		manager:     guardian.New(logger),
		logger:      logger,
		level:       level,
		levelHint:   hint,
	}
}

func (d *Daemon) Reload() error {
	cfg, err := config.Load(d.configPath)
	if err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil {
		return err
	}
	return d.manager.Apply(cfg)
}

func (d *Daemon) Statuses() []guardian.Status {
	return d.manager.Statuses()
}

func (d *Daemon) Logs(id string, lines int) (string, error) {
	return d.manager.Logs(id, lines)
}

func (d *Daemon) RuntimeInfo() (string, string) {
	return d.level, d.levelHint
}

func (d *Daemon) SetStopFunc(fn func()) {
	d.stopFunc = fn
}

func (d *Daemon) Shutdown() error {
	if d.stopFunc == nil {
		return errors.New("stop function is not configured")
	}
	d.stopOnce.Do(d.stopFunc)
	return nil
}

func (d *Daemon) Run(stop <-chan struct{}) error {
	if err := d.Reload(); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	server := ipc.NewServer(d.controlAddr, d)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.manager.Run(stop)
	}()

	err := server.Run(stop)
	wg.Wait()
	return err
}

func detectLevel() (string, string) {
	euid := os.Geteuid()
	pid := os.Getpid()

	if launchedAs(pid, fmt.Sprintf("gui/%d", os.Getuid()), service.Label) {
		return "user", "User mode: starts after user login. Use: sudo processgod-mac service install --system"
	}
	if launchedAs(pid, "system", service.Label) {
		return "system", "System mode: starts before user login."
	}

	if euid == 0 {
		return "system-manual", "Running as root manually. For managed pre-login boot use: sudo processgod-mac service install --system"
	}

	return "manual", "Manual mode. For auto-start after login use: processgod-mac service install"
}

func launchedAs(pid int, domain, label string) bool {
	cmd := exec.Command("launchctl", "print", domain+"/"+label)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	pattern := "pid = " + strconv.Itoa(pid)
	return strings.Contains(string(out), pattern)
}
