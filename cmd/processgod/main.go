package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/getlantern/systray"
	"github.com/lovitus/processgod-mac/internal/app"
	"github.com/lovitus/processgod-mac/internal/config"
	"github.com/lovitus/processgod-mac/internal/daemonctl"
	"github.com/lovitus/processgod-mac/internal/dashboard"
	"github.com/lovitus/processgod-mac/internal/guardian"
	"github.com/lovitus/processgod-mac/internal/ipc"
	"github.com/lovitus/processgod-mac/internal/service"
)

var version = "0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("error: %v", err)
	}
}

func run(args []string) error {
	configPath, err := config.EnsureDefaultConfig()
	if err != nil {
		return err
	}
	controlAddr := config.ControlAddress()

	if len(args) == 0 {
		if launchedFromAppBundle() {
			return runAppMode(configPath, controlAddr)
		}
		printUsage()
		return nil
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("processgod-mac %s\n", version)
		return nil
	case "daemon":
		return runDaemon(configPath, controlAddr)
	case "reload":
		resp, err := ipc.Send(controlAddr, ipc.Request{Action: "reload"})
		if err != nil {
			return err
		}
		if !resp.OK {
			return errors.New(resp.Error)
		}
		fmt.Println(resp.Message)
		return nil
	case "status":
		resp, err := ipc.Send(controlAddr, ipc.Request{Action: "status"})
		if err != nil {
			return err
		}
		if !resp.OK {
			return errors.New(resp.Error)
		}
		printStatus(resp.Status)
		return nil
	case "logs":
		return runLogs(controlAddr, args[1:])
	case "service":
		return runServiceCommand(args[1:])
	case "config":
		return runConfigCommand(configPath, args[1:])
	case "dashboard":
		return runDashboard(configPath, controlAddr, args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runAppMode(configPath, controlAddr string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		exe = strings.TrimSpace(exe)
	}

	workDir, err := config.EnsureAppSupportDir()
	if err != nil {
		return err
	}
	dashAddr := dashboardAddr()

	t := &trayApp{
		configPath:  configPath,
		controlAddr: controlAddr,
		exePath:     exe,
		workDir:     workDir,
		dashAddr:    dashAddr,
		dashboard:   "http://" + dashAddr,
	}
	go func() {
		s := &dashboard.Server{
			Addr:        dashAddr,
			ConfigPath:  configPath,
			ControlAddr: controlAddr,
			ExePath:     exe,
			WorkDir:     workDir,
		}
		if err := s.Run(false); err != nil {
			log.Printf("dashboard server stopped: %v", err)
		}
	}()
	systray.Run(t.onReady, t.onExit)
	return nil
}

func launchedFromAppBundle() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(exe, ".app/Contents/MacOS/")
}

func notify(title, message string) {
	script := fmt.Sprintf("display notification %s with title %s", appleScriptQuote(message), appleScriptQuote(title))
	_ = exec.Command("osascript", "-e", script).Run()
}

func appleScriptQuote(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return `"` + s + `"`
}

func runDaemon(configPath, controlAddr string) error {
	d := app.NewDaemon(configPath, controlAddr)
	stop := make(chan struct{})
	var once sync.Once
	closeStop := func() {
		once.Do(func() {
			close(stop)
		})
	}
	d.SetStopFunc(closeStop)

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		closeStop()
	}()

	return d.Run(stop)
}

func runLogs(controlAddr string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: processgod logs <id> [--lines N]")
	}
	id := args[0]
	lines := 200

	for i := 1; i < len(args); i++ {
		if args[i] == "--lines" {
			if i+1 >= len(args) {
				return fmt.Errorf("--lines requires a number")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				return fmt.Errorf("invalid --lines value %q", args[i+1])
			}
			lines = n
			i++
		}
	}

	resp, err := ipc.Send(controlAddr, ipc.Request{Action: "logs", ID: id, Lines: lines})
	if err != nil {
		return err
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	fmt.Println(resp.Logs)
	return nil
}

func runServiceCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: processgod service <install|start|stop|status|uninstall> [--system]")
	}

	system := false
	for _, a := range args[1:] {
		if a == "--system" {
			system = true
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		exe = strings.TrimSpace(exe)
	}

	workDir, err := config.EnsureAppSupportDir()
	if err != nil {
		return err
	}

	switch args[0] {
	case "install":
		if err := service.Install(exe, workDir, system); err != nil {
			return err
		}
		fmt.Println("service installed")
		return nil
	case "start":
		if err := service.Start(system); err != nil {
			return err
		}
		fmt.Println("service started")
		return nil
	case "stop":
		if err := service.Stop(system); err != nil {
			return err
		}
		fmt.Println("service stopped")
		return nil
	case "status":
		out, err := service.Status(system)
		if out != "" {
			fmt.Println(out)
		}
		return err
	case "uninstall":
		if err := service.Uninstall(system); err != nil {
			return err
		}
		fmt.Println("service uninstalled")
		return nil
	default:
		return fmt.Errorf("unknown service subcommand %q", args[0])
	}
}

func runConfigCommand(configPath string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: processgod config <init|path|validate|sample>")
	}

	switch args[0] {
	case "init":
		_, err := config.EnsureDefaultConfig()
		if err != nil {
			return err
		}
		fmt.Println(configPath)
		return nil
	case "path":
		fmt.Println(configPath)
		return nil
	case "validate":
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		if err := config.Validate(cfg); err != nil {
			return err
		}
		fmt.Println("config is valid")
		return nil
	case "sample":
		return writeSampleConfig(configPath)
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func runDashboard(configPath, controlAddr string, args []string) error {
	addr := dashboardAddr()
	openBrowser := true

	for _, a := range args {
		if strings.HasPrefix(a, "--addr=") {
			addr = strings.TrimSpace(strings.TrimPrefix(a, "--addr="))
		}
		if a == "--no-open" {
			openBrowser = false
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		exe = strings.TrimSpace(exe)
	}

	workDir, err := config.EnsureAppSupportDir()
	if err != nil {
		return err
	}

	s := &dashboard.Server{
		Addr:        addr,
		ConfigPath:  configPath,
		ControlAddr: controlAddr,
		ExePath:     exe,
		WorkDir:     workDir,
	}

	fmt.Printf("dashboard listening on http://%s\n", addr)
	return s.Run(openBrowser)
}

func dashboardAddr() string {
	if v := strings.TrimSpace(os.Getenv("PROCESSGOD_DASH_ADDR")); v != "" {
		return v
	}
	return "127.0.0.1:51090"
}

func writeSampleConfig(path string) error {
	sample := config.Config{Items: []config.Item{
		{
			ID:                 "sample-echo",
			ProcessName:        "Sample Echo",
			ExecPath:           "/bin/sh",
			Args:               []string{"-lc", "while true; do date; sleep 5; done"},
			Started:            true,
			OnlyOpenOnce:       false,
			NoWindow:           true,
			CronExpression:     "0 1 * * *",
			StopBeforeCronExec: true,
		},
	}}
	if err := config.Save(path, sample); err != nil {
		return err
	}
	fmt.Println("sample config written to", path)
	return nil
}

func printStatus(statuses []guardian.Status) {
	if len(statuses) == 0 {
		fmt.Println("no started items configured")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tRUNNING\tPID\tLAST START\tLAST ERROR")
	for _, s := range statuses {
		lastStart := "-"
		if !s.LastStart.IsZero() {
			lastStart = s.LastStart.Format("2006-01-02 15:04:05")
		}
		errText := s.LastError
		if errText == "" {
			errText = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%t\t%d\t%s\t%s\n", s.ID, s.Name, s.Running, s.PID, lastStart, errText)
	}
	_ = w.Flush()
}

func printUsage() {
	fmt.Println(`processgod-mac - macOS process guardian

Usage:
  processgod   (inside .app: opens tray app and starts guardian)
  processgod daemon
  processgod reload
  processgod status
  processgod logs <id> [--lines N]
  processgod config <init|path|validate|sample>
  processgod dashboard [--addr=127.0.0.1:51090] [--no-open]
  processgod service <install|start|stop|status|uninstall> [--system]
  processgod version

Notes:
  --system installs a LaunchDaemon in /Library/LaunchDaemons (boot startup, requires sudo).
  Without --system, service commands target a user LaunchAgent.`)
}

type trayApp struct {
	configPath  string
	controlAddr string
	exePath     string
	workDir     string
	dashAddr    string
	dashboard   string
}

func (t *trayApp) onReady() {
	systray.SetTitle("PG")
	systray.SetTooltip("ProcessGodMac")

	statusItem := systray.AddMenuItem("Status: checking...", "Daemon status")
	statusItem.Disable()
	activeItem := systray.AddMenuItem("Active: checking...", "Active process count")
	activeItem.Disable()
	levelItem := systray.AddMenuItem("Level: checking...", "Daemon service level")
	levelItem.Disable()
	hintItem := systray.AddMenuItem("Hint: checking...", "Service mode hint")
	hintItem.Disable()
	systray.AddSeparator()

	startItem := systray.AddMenuItem("Start Guardian (stopped)", "Start the guardian daemon")
	stopItem := systray.AddMenuItem("Stop Guardian (stopped)", "Stop the guardian daemon")
	reloadItem := systray.AddMenuItem("Reload Config", "Reload runtime config")
	showStatusItem := systray.AddMenuItem("Show Status", "Show short summary notification")
	openDashItem := systray.AddMenuItem("Open Dashboard", "Open web dashboard")
	openConfigItem := systray.AddMenuItem("Open Config", "Open config.json")
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Quit Tray", "Quit tray app")

	if err := t.ensureGuardianRunning(); err != nil {
		notify("ProcessGodMac", "Start failed: "+err.Error())
	} else {
		notify("ProcessGodMac", "Guardian is running.")
	}
	_ = exec.Command("open", t.dashboard).Run()

	go t.refreshStatus(statusItem, activeItem, levelItem, hintItem, startItem, stopItem, reloadItem, showStatusItem)

	go func() {
		for {
			select {
			case <-startItem.ClickedCh:
				if err := t.ensureGuardianRunning(); err != nil {
					notify("ProcessGodMac", "Start failed: "+err.Error())
				} else {
					notify("ProcessGodMac", "Guardian started.")
				}
			case <-stopItem.ClickedCh:
				if err := daemonctl.Stop(t.controlAddr); err != nil {
					notify("ProcessGodMac", "Stop failed: "+err.Error())
				} else {
					notify("ProcessGodMac", "Guardian stopped.")
				}
			case <-reloadItem.ClickedCh:
				resp, err := ipc.Send(t.controlAddr, ipc.Request{Action: "reload"})
				if err != nil {
					notify("ProcessGodMac", "Reload failed: "+err.Error())
					continue
				}
				if !resp.OK {
					notify("ProcessGodMac", "Reload failed: "+resp.Error)
					continue
				}
				notify("ProcessGodMac", "Config reloaded.")
			case <-showStatusItem.ClickedCh:
				notify("ProcessGodMac", t.statusSummary())
			case <-openDashItem.ClickedCh:
				_ = exec.Command("open", t.dashboard).Run()
			case <-openConfigItem.ClickedCh:
				_ = exec.Command("open", t.configPath).Run()
			case <-quitItem.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func (t *trayApp) onExit() {}

func (t *trayApp) ensureGuardianRunning() error {
	return daemonctl.EnsureRunning(t.controlAddr, t.exePath, t.workDir)
}

func (t *trayApp) statusSummary() string {
	online, running, total, level, _ := t.runtimeState()
	if !online {
		return "Guardian stopped."
	}
	if total == 0 {
		return fmt.Sprintf("Guardian running (%s). No started items configured.", level)
	}
	return fmt.Sprintf("Guardian running (%s). %d/%d items active.", level, running, total)
}

func (t *trayApp) refreshStatus(statusItem, activeItem, levelItem, hintItem, startItem, stopItem, reloadItem, showStatusItem *systray.MenuItem) {
	for {
		online, running, total, level, hint := t.runtimeState()

		if online {
			statusItem.SetTitle("Status: daemon running")
			activeItem.SetTitle(fmt.Sprintf("Active: %d/%d", running, total))
			levelItem.SetTitle("Level: " + level)
			hintItem.SetTitle("Hint: " + hint)
			startItem.SetTitle("Start Guardian (running)")
			stopItem.SetTitle("Stop Guardian (running)")
			startItem.Disable()
			stopItem.Enable()
			reloadItem.Enable()
			showStatusItem.Enable()
		} else {
			statusItem.SetTitle("Status: daemon stopped")
			activeItem.SetTitle("Active: 0/0")
			levelItem.SetTitle("Level: stopped")
			hintItem.SetTitle("Hint: start guardian first")
			startItem.SetTitle("Start Guardian (stopped)")
			stopItem.SetTitle("Stop Guardian (stopped)")
			startItem.Enable()
			stopItem.Disable()
			reloadItem.Disable()
			showStatusItem.Disable()
		}

		time.Sleep(2 * time.Second)
	}
}

func (t *trayApp) runtimeState() (online bool, running int, total int, level string, hint string) {
	resp, err := ipc.Send(t.controlAddr, ipc.Request{Action: "status"})
	if err != nil || !resp.OK {
		return false, 0, 0, "unknown", "Use sudo processgod-mac service install --system for pre-login boot."
	}
	total = len(resp.Status)
	for _, st := range resp.Status {
		if st.Running {
			running++
		}
	}
	level = resp.ServiceLevel
	if level == "" {
		level = "manual"
	}
	hint = resp.ServiceHint
	if hint == "" {
		hint = "Use sudo processgod-mac service install --system for pre-login boot."
	}
	return true, running, total, level, hint
}
