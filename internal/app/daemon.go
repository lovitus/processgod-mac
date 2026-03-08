package app

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/lovitus/processgod-mac/internal/config"
	"github.com/lovitus/processgod-mac/internal/guardian"
	"github.com/lovitus/processgod-mac/internal/ipc"
)

type Daemon struct {
	configPath  string
	controlAddr string
	manager     *guardian.Manager
	logger      *log.Logger
	stopOnce    sync.Once
	stopFunc    func()
}

func NewDaemon(configPath, controlAddr string) *Daemon {
	logger := log.New(os.Stdout, "[processgod] ", log.LstdFlags)
	return &Daemon{
		configPath:  configPath,
		controlAddr: controlAddr,
		manager:     guardian.New(logger),
		logger:      logger,
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
