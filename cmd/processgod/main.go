package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"text/tabwriter"

	"github.com/lovitus/processgod-mac/internal/api"
	"github.com/lovitus/processgod-mac/internal/app"
	"github.com/lovitus/processgod-mac/internal/config"
	"github.com/lovitus/processgod-mac/internal/ipc"
	"github.com/lovitus/processgod-mac/internal/runtimepaths"
	"github.com/lovitus/processgod-mac/internal/service"
)

var version = "0.4.0-dev"

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
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("processgod-mac %s\n", version)
		return nil
	case "daemon":
		return runDaemon(args[1:])
	case "status":
		return runStatus(args[1:])
	case "logs":
		return runLogs(args[1:])
	case "restart":
		return runIDMethod("process.restart", args[1:])
	case "pause":
		return runSimpleMethod("guardian.pause", args[1:])
	case "resume":
		return runSimpleMethod("guardian.resume", args[1:])
	case "reload":
		return runSimpleMethod("config.reload", args[1:])
	case "service", "legacy-service":
		return runServiceCommand(args[1:])
	case "config":
		return runConfigCommand(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runDaemon(args []string) error {
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	scopeValue := flags.String("scope", "", "user or system")
	if err := flags.Parse(args); err != nil {
		return err
	}
	system := os.Geteuid() == 0
	if *scopeValue != "" {
		switch *scopeValue {
		case "user":
			system = false
		case "system":
			system = true
		default:
			return fmt.Errorf("invalid scope %q", *scopeValue)
		}
	}
	configPath, err := runtimepaths.ConfigPath(system)
	if err != nil {
		return err
	}
	if !system {
		if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
			if err := config.Save(configPath, config.Config{}); err != nil {
				return err
			}
		}
	}
	socketPath, err := runtimepaths.SocketPath(system)
	if err != nil {
		return err
	}
	scope := ipc.ScopeUser
	if system {
		scope = ipc.ScopeSystem
	}
	daemon := app.NewDaemon(configPath, socketPath, scope)
	stop := make(chan struct{})
	var once sync.Once
	closeStop := func() { once.Do(func() { close(stop) }) }
	daemon.SetStopFunc(closeStop)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		closeStop()
	}()
	return daemon.Run(stop)
}

func runStatus(args []string) error {
	client, err := clientFromArgs("status", args)
	if err != nil {
		return err
	}
	var snapshot api.RuntimeSnapshot
	if err := client.Call(context.Background(), "status.list", nil, &snapshot); err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATE\tPID\tLAST START\tERROR")
	for _, process := range snapshot.Processes {
		lastStart := "-"
		if process.LastStart != nil {
			lastStart = process.LastStart.Local().Format("2006-01-02 15:04:05")
		}
		errText := process.Error
		if errText == "" {
			errText = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n", process.ID, process.Name, process.State, process.PID, lastStart, errText)
	}
	return w.Flush()
}

func runLogs(args []string) error {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	system := flags.Bool("system", false, "connect to system daemon")
	lines := flags.Int("lines", 0, "maximum lines per buffer")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: processgod-mac logs [--system] [--lines N] <id>")
	}
	client, err := clientForScope(*system)
	if err != nil {
		return err
	}
	var snapshot api.LogSnapshot
	if err := client.Call(context.Background(), "logs.snapshot", map[string]any{"id": flags.Arg(0), "lines": *lines}, &snapshot); err != nil {
		return err
	}
	fmt.Printf("# memory-only: error_warning=%d/%d standard_other=%d/%d total_seen=%d line_max_bytes=%d\n", snapshot.ErrorWarning.Kept, snapshot.ErrorWarning.Capacity, snapshot.StandardOther.Kept, snapshot.StandardOther.Capacity, snapshot.TotalSeen, snapshot.LineMaxBytes)
	fmt.Println("\n[error_warning]")
	for _, entry := range snapshot.ErrorWarning.Entries {
		fmt.Printf("E#%d %s\n", entry.Sequence, entry.Text)
	}
	fmt.Println("\n[standard_other]")
	for _, entry := range snapshot.StandardOther.Entries {
		fmt.Printf("S#%d %s\n", entry.Sequence, entry.Text)
	}
	return nil
}

func runIDMethod(method string, args []string) error {
	flags := flag.NewFlagSet(method, flag.ContinueOnError)
	system := flags.Bool("system", false, "connect to system daemon")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("%s requires a process id", method)
	}
	client, err := clientForScope(*system)
	if err != nil {
		return err
	}
	return client.Call(context.Background(), method, map[string]string{"id": flags.Arg(0)}, nil)
}

func runSimpleMethod(method string, args []string) error {
	client, err := clientFromArgs(method, args)
	if err != nil {
		return err
	}
	return client.Call(context.Background(), method, map[string]uint64{"expectedRevision": 0}, nil)
}

func clientFromArgs(name string, args []string) (*ipc.Client, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	system := flags.Bool("system", false, "connect to system daemon")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	return clientForScope(*system)
}

func clientForScope(system bool) (*ipc.Client, error) {
	path, err := runtimepaths.SocketPath(system)
	if err != nil {
		return nil, err
	}
	return &ipc.Client{SocketPath: path}, nil
}

func runConfigCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: processgod-mac config <path|validate|sample> [--system]")
	}
	system := false
	for _, arg := range args[1:] {
		if arg == "--system" {
			system = true
		}
	}
	path, err := runtimepaths.ConfigPath(system)
	if err != nil {
		return err
	}
	switch args[0] {
	case "path":
		fmt.Println(path)
		return nil
	case "validate":
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		if err := config.Validate(cfg); err != nil {
			return err
		}
		fmt.Println("config is valid")
		return nil
	case "sample":
		return writeSample(path)
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func writeSample(path string) error {
	sample := config.Config{Items: []config.Item{{
		ID: "sample-echo", ProcessName: "Sample Echo", ExecPath: "/bin/sh",
		Args:          []string{"-lc", "while true; do date; sleep 5; done"},
		StartupParams: "-lc 'while true; do date; sleep 5; done'", Started: true,
	}}}
	if err := config.Save(path, sample); err != nil {
		return err
	}
	fmt.Println("sample config written to", path)
	return nil
}

func runServiceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: processgod-mac service <install|start|stop|status|uninstall> [--system]")
	}
	system := false
	for _, arg := range args[1:] {
		if arg == "--system" {
			system = true
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, _ = filepath.EvalSymlinks(executable)
	root, err := serviceWorkingRoot(system)
	if err != nil {
		return err
	}
	switch args[0] {
	case "install":
		return service.Install(executable, root, system)
	case "start":
		return service.Start(system)
	case "stop":
		return service.Stop(system)
	case "uninstall":
		return service.Uninstall(system)
	case "status":
		output, err := service.Status(system)
		fmt.Print(output)
		return err
	default:
		return fmt.Errorf("unknown service command %q", args[0])
	}
}

func serviceWorkingRoot(system bool) (string, error) {
	if system {
		return runtimepaths.SystemRoot, nil
	}
	return runtimepaths.UserRoot()
}

func printUsage() {
	fmt.Println(`processgod-mac - ProcessGod native helper and CLI

Usage:
  processgod-mac daemon [--scope user|system]
  processgod-mac status [--system]
  processgod-mac logs [--system] [--lines N] <id>
  processgod-mac restart [--system] <id>
  processgod-mac pause|resume|reload [--system]
  processgod-mac config <path|validate|sample> [--system]
  processgod-mac service <install|start|stop|status|uninstall> [--system]
  processgod-mac version`)
}
