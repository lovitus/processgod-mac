package main

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/getlantern/systray"
	"github.com/lovitus/processgod-mac/internal/config"
	"github.com/lovitus/processgod-mac/internal/daemonctl"
	"github.com/lovitus/processgod-mac/internal/guardian"
	"github.com/lovitus/processgod-mac/internal/ipc"
	"github.com/lovitus/processgod-mac/internal/service"
)

const maxTrayTasks = 32

type trayApp struct {
	configPath  string
	controlAddr string
	exePath     string
	workDir     string
	dashAddr    string
	dashboard   string
	refreshCh   chan struct{}

	summaryItem  *systray.MenuItem
	processTitle *systray.MenuItem
	guardianItem *systray.MenuItem
	startupItem  *systray.MenuItem
	moreItem     *systray.MenuItem
	taskSlots    []*trayTaskSlot
}

type trayTaskSlot struct {
	mu sync.RWMutex
	id string

	parent  *systray.MenuItem
	status  *systray.MenuItem
	toggle  *systray.MenuItem
	restart *systray.MenuItem
	logs    *systray.MenuItem
	edit    *systray.MenuItem
	delete  *systray.MenuItem
}

func (t *trayApp) onReady() {
	t.refreshCh = make(chan struct{}, 1)
	systray.SetTitle("PG")
	systray.SetTooltip("ProcessGodMac")

	t.summaryItem = systray.AddMenuItem("Guardian: checking...", "Guardian status")
	t.summaryItem.Disable()
	t.startupItem = systray.AddMenuItem("Startup: checking...", "Choose when the guardian starts")
	t.addStartupMenu(t.startupItem)
	systray.AddSeparator()

	t.processTitle = systray.AddMenuItem("Processes", "Configured processes")
	t.processTitle.Disable()
	for i := 0; i < maxTrayTasks; i++ {
		slot := newTrayTaskSlot()
		t.taskSlots = append(t.taskSlots, slot)
		go slot.run(t)
	}
	t.moreItem = systray.AddMenuItem("More processes...", "Open the full process manager")
	t.moreItem.Hide()
	systray.AddSeparator()

	addItem := systray.AddMenuItem("Add Process...", "Open a focused new process form")
	manageItem := systray.AddMenuItem("Manage Processes...", "Open the full process manager")
	t.guardianItem = systray.AddMenuItem("Start Guardian", "Start or stop the guardian daemon")
	reloadItem := systray.AddMenuItem("Reload Configuration", "Reload config.json in the guardian")
	openConfigItem := systray.AddMenuItem("Open config.json", "Open the raw configuration file")
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Quit ProcessGodMac", "Close the menu bar app")

	go func() {
		for {
			select {
			case <-addItem.ClickedCh:
				t.openDashboard("/?new=1")
			case <-manageItem.ClickedCh:
				t.openDashboard("/")
			case <-t.moreItem.ClickedCh:
				t.openDashboard("/")
			case <-t.guardianItem.ClickedCh:
				t.toggleGuardian()
			case <-reloadItem.ClickedCh:
				t.reloadGuardian(true)
			case <-openConfigItem.ClickedCh:
				_ = exec.Command("open", t.configPath).Run()
			case <-quitItem.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	go t.refreshLoop()
	go func() {
		if err := t.ensureGuardianRunning(); err != nil {
			notify("ProcessGodMac", "Guardian start failed: "+err.Error())
		}
		t.requestRefresh()
	}()
}

func (t *trayApp) onExit() {}

func newTrayTaskSlot() *trayTaskSlot {
	parent := systray.AddMenuItem("Process", "Process controls")
	s := &trayTaskSlot{
		parent:  parent,
		status:  parent.AddSubMenuItem("Status: checking...", "Current process status"),
		toggle:  parent.AddSubMenuItem("Enable Guard", "Enable or disable this process"),
		restart: parent.AddSubMenuItem("Restart Now", "Stop and immediately start this process"),
		logs:    parent.AddSubMenuItem("View Memory Logs...", "Open retained memory-only logs"),
		edit:    parent.AddSubMenuItem("Edit...", "Edit process settings"),
		delete:  parent.AddSubMenuItem("Delete...", "Delete this process configuration"),
	}
	s.status.Disable()
	s.parent.Hide()
	return s
}

func (s *trayTaskSlot) run(t *trayApp) {
	for {
		select {
		case <-s.toggle.ClickedCh:
			if id := s.currentID(); id != "" {
				t.runTaskAction("Update guard", func() error { return t.toggleTask(id) })
			}
		case <-s.restart.ClickedCh:
			if id := s.currentID(); id != "" {
				t.runTaskAction("Restart", func() error { return t.restartTask(id) })
			}
		case <-s.logs.ClickedCh:
			if id := s.currentID(); id != "" {
				t.openDashboard("/logs?id=" + url.QueryEscape(id) + "&lines=120")
			}
		case <-s.edit.ClickedCh:
			if id := s.currentID(); id != "" {
				t.openDashboard("/?edit=" + url.QueryEscape(id))
			}
		case <-s.delete.ClickedCh:
			if id := s.currentID(); id != "" {
				t.runTaskAction("Delete", func() error { return t.deleteTask(id) })
			}
		}
	}
}

func (s *trayTaskSlot) currentID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

func (s *trayTaskSlot) update(item config.Item, st guardian.Status, hasStatus, online bool) {
	s.mu.Lock()
	s.id = item.ID
	s.mu.Unlock()

	name := item.ProcessName
	if name == "" {
		name = item.ID
	}
	state := "Disabled"
	detail := "Guard disabled"
	if item.Started {
		state = "Waiting"
		detail = "Enabled; waiting for guardian"
		if !online {
			state = "Offline"
			detail = "Guardian is not running"
		} else if hasStatus && st.Running {
			state = fmt.Sprintf("Running (PID %d)", st.PID)
			detail = state
		} else if hasStatus && st.LastError != "" {
			state = "Error"
			detail = shortMenuText(st.LastError)
		} else if hasStatus && item.OnlyOpenOnce && !st.LastExit.IsZero() {
			state = "Completed"
			detail = "Start-once process completed"
		} else if hasStatus && strings.TrimSpace(item.CronExpression) != "" && !item.StopBeforeCronExec {
			state = "Scheduled"
			detail = "Waiting for the next cron trigger"
		}
	}

	s.parent.SetTitle(fmt.Sprintf("%s - %s", name, state))
	s.parent.SetTooltip(strings.TrimSpace(item.ExecPath + " " + item.StartupParams))
	s.status.SetTitle("Status: " + detail)
	if hasStatus && st.Running {
		s.parent.Check()
	} else {
		s.parent.Uncheck()
	}
	if item.Started {
		s.toggle.SetTitle("Disable Guard and Stop")
	} else {
		s.toggle.SetTitle("Enable Guard")
	}
	if online && item.Started {
		s.restart.Enable()
	} else {
		s.restart.Disable()
	}
	if online && hasStatus {
		s.logs.Enable()
	} else {
		s.logs.Disable()
	}
	s.parent.Show()
}

func (s *trayTaskSlot) clear() {
	s.mu.Lock()
	s.id = ""
	s.mu.Unlock()
	s.parent.Hide()
}

func (t *trayApp) refreshLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		t.refreshMenu()
		select {
		case <-ticker.C:
		case <-t.refreshCh:
		}
	}
}

func (t *trayApp) refreshMenu() {
	cfg, cfgErr := config.Load(t.configPath)
	resp, statusErr := ipc.Send(t.controlAddr, ipc.Request{Action: "status"})
	online := statusErr == nil && resp.OK

	if cfgErr != nil {
		t.summaryItem.SetTitle("Configuration error")
		t.summaryItem.SetTooltip(cfgErr.Error())
		for _, slot := range t.taskSlots {
			slot.clear()
		}
		return
	}

	sort.SliceStable(cfg.Items, func(i, j int) bool {
		return strings.ToLower(cfg.Items[i].ProcessName) < strings.ToLower(cfg.Items[j].ProcessName)
	})
	statusByID := make(map[string]guardian.Status, len(resp.Status))
	running := 0
	for _, st := range resp.Status {
		statusByID[st.ID] = st
		if st.Running {
			running++
		}
	}
	enabled := 0
	for _, item := range cfg.Items {
		if item.Started {
			enabled++
		}
	}

	if online {
		level := resp.ServiceLevel
		if level == "" {
			level = "manual"
		}
		t.summaryItem.SetTitle(fmt.Sprintf("Guardian: Running - %d/%d active", running, enabled))
		t.summaryItem.SetTooltip(resp.ServiceHint)
		t.guardianItem.SetTitle("Stop Guardian")
		t.startupItem.SetTitle("Startup: " + startupLabel(level))
		systray.SetTitle(fmt.Sprintf("PG %d/%d", running, enabled))
	} else {
		t.summaryItem.SetTitle("Guardian: Stopped")
		t.summaryItem.SetTooltip("Click Start Guardian to run configured processes")
		t.guardianItem.SetTitle("Start Guardian")
		switch {
		case service.Installed(true):
			t.startupItem.SetTitle("Startup: System (before login)")
		case service.Installed(false):
			t.startupItem.SetTitle("Startup: User (after login)")
		default:
			t.startupItem.SetTitle("Startup: Manual")
		}
		systray.SetTitle("PG Off")
	}

	t.processTitle.SetTitle(fmt.Sprintf("Processes - %d configured", len(cfg.Items)))
	for i, slot := range t.taskSlots {
		if i >= len(cfg.Items) {
			slot.clear()
			continue
		}
		item := cfg.Items[i]
		st, ok := statusByID[item.ID]
		slot.update(item, st, ok, online)
	}
	if len(cfg.Items) > len(t.taskSlots) {
		t.moreItem.SetTitle(fmt.Sprintf("Manage %d more processes...", len(cfg.Items)-len(t.taskSlots)))
		t.moreItem.Show()
	} else {
		t.moreItem.Hide()
	}
}

func (t *trayApp) addStartupMenu(parent *systray.MenuItem) {
	userItem := parent.AddSubMenuItem("Start after login (User)", "Install a user LaunchAgent")
	systemItem := parent.AddSubMenuItem("Start before login (System)...", "Install a system LaunchDaemon with administrator approval")
	removeItem := parent.AddSubMenuItem("Remove automatic startup...", "Remove the installed launchd service")

	go func() {
		for {
			select {
			case <-userItem.ClickedCh:
				t.runTaskAction("User startup", t.installUserService)
			case <-systemItem.ClickedCh:
				t.runTaskAction("System startup", t.installSystemService)
			case <-removeItem.ClickedCh:
				t.runTaskAction("Remove startup", t.removeStartupService)
			}
		}
	}()
}

func (t *trayApp) installUserService() error {
	_ = daemonctl.Stop(t.controlAddr)
	if service.Installed(true) {
		if err := t.runAdminService("uninstall", "--system"); err != nil {
			return err
		}
	}
	_ = service.Uninstall(false)
	return service.Install(t.exePath, t.workDir, false)
}

func (t *trayApp) installSystemService() error {
	_ = daemonctl.Stop(t.controlAddr)
	_ = service.Uninstall(false)
	return t.runAdminService("install", "--system")
}

func (t *trayApp) removeStartupService() error {
	removed := false
	if err := service.Uninstall(false); err == nil {
		removed = true
	}
	if service.Installed(true) {
		if err := t.runAdminService("uninstall", "--system"); err != nil {
			return err
		}
		removed = true
	}
	if !removed {
		return errors.New("no automatic startup service was found")
	}
	return nil
}

func (t *trayApp) runAdminService(args ...string) error {
	parts := []string{shellQuote(t.exePath), "service"}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	script := "do shell script " + appleScriptQuote(strings.Join(parts, " ")) + " with administrator privileges"
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

func (t *trayApp) toggleGuardian() {
	var err error
	if daemonctl.IsRunning(t.controlAddr) {
		resp, statusErr := ipc.Send(t.controlAddr, ipc.Request{Action: "status"})
		if statusErr == nil && resp.OK && strings.HasPrefix(resp.ServiceLevel, "system") {
			err = t.runAdminService("stop", "--system")
		} else {
			err = daemonctl.Stop(t.controlAddr)
		}
	} else if service.Installed(true) {
		err = t.runAdminService("start", "--system")
	} else {
		err = t.ensureGuardianRunning()
	}
	if err != nil {
		notify("ProcessGodMac", "Guardian action failed: "+err.Error())
	}
	t.requestRefresh()
}

func (t *trayApp) ensureGuardianRunning() error {
	return daemonctl.EnsureRunning(t.controlAddr, t.exePath, t.workDir)
}

func (t *trayApp) reloadGuardian(showNotification bool) error {
	if !daemonctl.IsRunning(t.controlAddr) {
		return nil
	}
	resp, err := ipc.Send(t.controlAddr, ipc.Request{Action: "reload"})
	if err != nil {
		if showNotification {
			notify("ProcessGodMac", "Reload failed: "+err.Error())
		}
		return err
	}
	if !resp.OK {
		err = errors.New(resp.Error)
		if showNotification {
			notify("ProcessGodMac", "Reload failed: "+resp.Error)
		}
		return err
	}
	t.requestRefresh()
	return nil
}

func (t *trayApp) toggleTask(id string) error {
	cfg, err := config.Load(t.configPath)
	if err != nil {
		return err
	}
	found := false
	for i := range cfg.Items {
		if cfg.Items[i].ID == id {
			cfg.Items[i].Started = !cfg.Items[i].Started
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("process %q was not found", id)
	}
	if err := config.Save(t.configPath, cfg); err != nil {
		return err
	}
	return t.reloadGuardian(false)
}

func (t *trayApp) restartTask(id string) error {
	resp, err := ipc.Send(t.controlAddr, ipc.Request{Action: "restart", ID: id})
	if err != nil {
		return err
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	return nil
}

func (t *trayApp) deleteTask(id string) error {
	cfg, err := config.Load(t.configPath)
	if err != nil {
		return err
	}
	name := id
	for _, item := range cfg.Items {
		if item.ID == id && item.ProcessName != "" {
			name = item.ProcessName
			break
		}
	}
	if !confirmDialog("Delete process '" + name + "'? This also stops the process.") {
		return nil
	}

	items := make([]config.Item, 0, len(cfg.Items))
	found := false
	for _, item := range cfg.Items {
		if item.ID == id {
			found = true
			continue
		}
		items = append(items, item)
	}
	if !found {
		return fmt.Errorf("process %q was not found", id)
	}
	cfg.Items = items
	if err := config.Save(t.configPath, cfg); err != nil {
		return err
	}
	return t.reloadGuardian(false)
}

func (t *trayApp) runTaskAction(name string, action func() error) {
	go func() {
		if err := action(); err != nil {
			notify("ProcessGodMac", name+" failed: "+err.Error())
		}
		t.requestRefresh()
	}()
}

func (t *trayApp) requestRefresh() {
	select {
	case t.refreshCh <- struct{}{}:
	default:
	}
}

func (t *trayApp) openDashboard(path string) {
	_ = exec.Command("open", strings.TrimRight(t.dashboard, "/")+path).Run()
}

func confirmDialog(message string) bool {
	script := "display dialog " + appleScriptQuote(message) + " with title \"ProcessGodMac\" buttons {\"Cancel\", \"Delete\"} default button \"Delete\" cancel button \"Cancel\" with icon caution"
	return exec.Command("osascript", "-e", script).Run() == nil
}

func startupLabel(level string) string {
	switch level {
	case "system", "system-manual":
		return "System (before login)"
	case "user":
		return "User (after login)"
	default:
		return "Manual"
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func shortMenuText(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	const limit = 100
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}
