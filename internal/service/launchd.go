package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const Label = "com.lovitus.processgod.mac"

func Install(binaryPath, workingDir string, system bool) error {
	if system && os.Geteuid() != 0 {
		return fmt.Errorf("system install requires root (run with sudo)")
	}

	plistPath, err := plistPath(system)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return fmt.Errorf("create plist directory: %w", err)
	}

	binaryPath, err = filepath.Abs(binaryPath)
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	workingDir, err = filepath.Abs(workingDir)
	if err != nil {
		return fmt.Errorf("resolve working dir: %w", err)
	}

	content := renderPlist(binaryPath, workingDir)
	if err := os.WriteFile(plistPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	domain := launchDomain(system)
	_ = runLaunchctl("bootout", domain, plistPath)
	if err := runLaunchctl("bootstrap", domain, plistPath); err != nil {
		return err
	}
	if err := runLaunchctl("enable", domain+"/"+Label); err != nil {
		return err
	}
	if err := runLaunchctl("kickstart", "-k", domain+"/"+Label); err != nil {
		return err
	}

	return nil
}

func Start(system bool) error {
	domain := launchDomain(system)
	return runLaunchctl("kickstart", "-k", domain+"/"+Label)
}

func Stop(system bool) error {
	plist, err := plistPath(system)
	if err != nil {
		return err
	}
	domain := launchDomain(system)
	return runLaunchctl("bootout", domain, plist)
}

func Uninstall(system bool) error {
	if system && os.Geteuid() != 0 {
		return fmt.Errorf("system uninstall requires root (run with sudo)")
	}
	plist, err := plistPath(system)
	if err != nil {
		return err
	}
	domain := launchDomain(system)
	_ = runLaunchctl("bootout", domain, plist)

	if err := os.Remove(plist); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

func Status(system bool) (string, error) {
	domain := launchDomain(system)
	cmd := exec.Command("launchctl", "print", domain+"/"+Label)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("launchctl print failed: %w", err)
	}
	return out.String(), nil
}

func plistPath(system bool) (string, error) {
	if system {
		return filepath.Join("/Library", "LaunchDaemons", Label+".plist"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

func launchDomain(system bool) string {
	if system {
		return "system"
	}
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func renderPlist(binaryPath, workingDir string) string {
	escapedBinary := xmlEscape(binaryPath)
	escapedWorkDir := xmlEscape(workingDir)

	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + Label + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + escapedBinary + `</string>
    <string>daemon</string>
  </array>
  <key>WorkingDirectory</key>
  <string>` + escapedWorkDir + `</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/dev/null</string>
  <key>StandardErrorPath</key>
  <string>/dev/null</string>
</dict>
</plist>
`
}

func runLaunchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("launchctl %s: %s", strings.Join(args, " "), msg)
	}
	return nil
}

func xmlEscape(v string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(v)
}
