package guardian

import (
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lovitus/processgod-mac/internal/config"
)

func TestCronTriggeredRunWithoutAutoRestart(t *testing.T) {
	m := New(log.New(os.Stdout, "", 0))
	t.Cleanup(m.shutdown)

	script := writeScript(t, "#!/bin/sh\necho run\nsleep 2\n")
	cfg := config.Config{Items: []config.Item{{
		ID:                 "cron-task",
		ProcessName:        "CronTask",
		ExecPath:           script,
		Started:            true,
		CronExpression:     "0 1 * * *",
		StopBeforeCronExec: false,
	}}}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}

	noMatch := time.Date(2026, 3, 8, 0, 59, 0, 0, time.Local)
	m.tick(noMatch)

	m.mu.Lock()
	p := m.procs["cron-task"]
	if p.running {
		m.mu.Unlock()
		t.Fatalf("expected not running before cron match")
	}
	m.mu.Unlock()

	match := time.Date(2026, 3, 8, 1, 0, 0, 0, time.Local)
	m.tick(match)
	waitFor(t, 2*time.Second, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.procs["cron-task"].running
	})

	waitFor(t, 6*time.Second, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return !m.procs["cron-task"].running
	})
	m.tick(match.Add(10 * time.Second))

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.procs["cron-task"].running {
		t.Fatalf("expected process to stay stopped after exit when stopBeforeCronExec=false")
	}
}

func TestOnlyOpenOnceDoesNotRestart(t *testing.T) {
	m := New(log.New(os.Stdout, "", 0))
	t.Cleanup(m.shutdown)

	script := writeScript(t, "#!/bin/sh\necho once\n")
	cfg := config.Config{Items: []config.Item{{
		ID:           "once-task",
		ProcessName:  "OnceTask",
		ExecPath:     script,
		Started:      true,
		OnlyOpenOnce: true,
	}}}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}

	now := time.Now()
	m.tick(now)
	waitFor(t, 2*time.Second, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return !m.procs["once-task"].running && m.procs["once-task"].startedOnce
	})

	m.tick(now.Add(3 * time.Second))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.procs["once-task"].running {
		t.Fatalf("expected only-open-once process to remain stopped")
	}
}

func TestCronRestartTriggersOncePerMinute(t *testing.T) {
	m := New(log.New(os.Stdout, "", 0))
	t.Cleanup(m.shutdown)

	script := writeScript(t, "#!/bin/sh\nwhile true; do sleep 1; done\n")
	cfg := config.Config{Items: []config.Item{{
		ID:                 "restart-task",
		ProcessName:        "RestartTask",
		ExecPath:           script,
		Started:            true,
		CronExpression:     "* * * * *",
		StopBeforeCronExec: true,
	}}}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}

	now := time.Date(2026, 3, 8, 10, 0, 5, 0, time.Local)
	m.tick(now)
	pid1 := waitPID(t, m, "restart-task")

	m.tick(now.Add(20 * time.Second))
	pid2 := waitPID(t, m, "restart-task")
	if pid2 != pid1 {
		t.Fatalf("expected same pid within same minute; got %d then %d", pid1, pid2)
	}

	m.tick(now.Add(61 * time.Second))
	pid3 := waitPID(t, m, "restart-task")
	if pid3 == pid2 {
		t.Fatalf("expected new pid after next minute cron trigger")
	}
}

func TestRestartStartsNewProcess(t *testing.T) {
	m := New(log.New(os.Stdout, "", 0))
	t.Cleanup(m.shutdown)

	script := writeScript(t, "#!/bin/sh\nwhile true; do sleep 1; done\n")
	cfg := config.Config{Items: []config.Item{{
		ID:       "manual-restart",
		ExecPath: script,
		Started:  true,
		NoWindow: true,
	}}}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	m.tick(time.Now())
	pid1 := waitPID(t, m, "manual-restart")

	if err := m.Restart("manual-restart"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	pid2 := waitPID(t, m, "manual-restart")
	if pid2 == pid1 {
		t.Fatalf("expected a new pid, still %d", pid1)
	}
}

func TestPausedConfigStopsAndResumesProcesses(t *testing.T) {
	m := New(log.New(os.Stdout, "", 0))
	t.Cleanup(m.shutdown)
	script := writeScript(t, "#!/bin/sh\nwhile true; do sleep 1; done\n")
	cfg := config.Config{GuardianPaused: true, Items: []config.Item{{ID: "paused", ExecPath: script, Started: true}}}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("apply paused: %v", err)
	}
	m.tick(time.Now())
	m.mu.Lock()
	if m.procs["paused"].running {
		m.mu.Unlock()
		t.Fatalf("paused manager started a process")
	}
	m.mu.Unlock()

	cfg.GuardianPaused = false
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("apply resumed: %v", err)
	}
	m.tick(time.Now())
	waitPID(t, m, "paused")
}

func TestDisablingTaskRetainsBoundedLogs(t *testing.T) {
	m := New(log.New(os.Stdout, "", 0))
	cfg := config.Config{Items: []config.Item{{ID: "retained", ExecPath: "/bin/echo", Started: true}}}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("apply enabled: %v", err)
	}
	m.mu.Lock()
	m.procs["retained"].logs.Add("before disable", false)
	m.mu.Unlock()

	cfg.Items[0].Started = false
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("apply disabled: %v", err)
	}
	snapshot, err := m.LogSnapshot("retained", 0)
	if err != nil || snapshot.StandardOther.Kept != 1 || snapshot.StandardOther.Entries[0].Text != "before disable" {
		t.Fatalf("logs were not retained: snapshot=%+v err=%v", snapshot, err)
	}
	if err := m.Restart("retained"); err == nil {
		t.Fatal("disabled task should not restart")
	}
}

func writeScript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func waitPID(t *testing.T, m *Manager, id string) int {
	t.Helper()
	var pid int
	waitFor(t, 2*time.Second, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		p := m.procs[id]
		pid = p.pid
		return p.running && p.pid > 0
	})
	return pid
}
