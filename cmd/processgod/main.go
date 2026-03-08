package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/lovitus/processgod-mac/internal/app"
	"github.com/lovitus/processgod-mac/internal/config"
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
	if len(args) == 0 {
		printUsage()
		return nil
	}

	configPath, err := config.EnsureDefaultConfig()
	if err != nil {
		return err
	}
	controlAddr := config.ControlAddress()

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
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runDaemon(configPath, controlAddr string) error {
	d := app.NewDaemon(configPath, controlAddr)
	stop := make(chan struct{})

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		close(stop)
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
  processgod daemon
  processgod reload
  processgod status
  processgod logs <id> [--lines N]
  processgod config <init|path|validate|sample>
  processgod service <install|start|stop|status|uninstall> [--system]
  processgod version

Notes:
  --system installs a LaunchDaemon in /Library/LaunchDaemons (boot startup, requires sudo).
  Without --system, service commands target a user LaunchAgent.`)
}
