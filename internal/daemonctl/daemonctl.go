package daemonctl

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lovitus/processgod-mac/internal/ipc"
	"github.com/lovitus/processgod-mac/internal/service"
)

func IsRunning(controlAddr string) bool {
	resp, err := ipc.Send(controlAddr, ipc.Request{Action: "ping"})
	return err == nil && resp.OK
}

func WaitPing(controlAddr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if IsRunning(controlAddr) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not answer ping within %s", timeout)
}

func EnsureRunning(controlAddr, exePath, workDir string) error {
	if IsRunning(controlAddr) {
		return nil
	}

	if err := service.Install(exePath, workDir, false); err == nil {
		if err := WaitPing(controlAddr, 6*time.Second); err == nil {
			return nil
		}
	}

	if err := StartDetached(exePath, workDir); err != nil {
		return err
	}
	if err := WaitPing(controlAddr, 5*time.Second); err != nil {
		return fmt.Errorf("daemon failed to start; check %s", filepath.Join(workDir, "app-launch.log"))
	}
	return nil
}

func Stop(controlAddr string) error {
	stoppedByService := false
	if err := service.Stop(false); err == nil {
		stoppedByService = true
	}
	_, shutdownErr := ipc.Send(controlAddr, ipc.Request{Action: "shutdown"})
	if stoppedByService || shutdownErr == nil {
		return nil
	}
	return shutdownErr
}

func StartDetached(exePath, workDir string) error {
	logPath := filepath.Join(workDir, "app-launch.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}

	cmd := exec.Command(exePath, "daemon")
	cmd.Dir = workDir
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		_ = f.Close()
		return fmt.Errorf("start detached daemon: %w", err)
	}
	_ = cmd.Process.Release()
	_ = f.Close()
	return nil
}
