package runtimepaths

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	SystemRoot       = "/Library/Application Support/ProcessGodMac"
	SystemSocketPath = "/var/run/processgod-mac/system.sock"
)

func UserRoot() (string, error) {
	if override := strings.TrimSpace(os.Getenv("PROCESSGOD_HOME")); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "ProcessGodMac"), nil
}

func UserSocketPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv("PROCESSGOD_SOCKET")); override != "" {
		return override, nil
	}
	root, err := UserRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "run", "user.sock"), nil
}

func ConfigPath(system bool) (string, error) {
	if system {
		if override := strings.TrimSpace(os.Getenv("PROCESSGOD_HOME")); override != "" {
			return filepath.Join(override, "config.json"), nil
		}
		return filepath.Join(SystemRoot, "config.json"), nil
	}
	root, err := UserRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "config.json"), nil
}

func SocketPath(system bool) (string, error) {
	if override := strings.TrimSpace(os.Getenv("PROCESSGOD_SOCKET")); override != "" {
		return override, nil
	}
	if system {
		return SystemSocketPath, nil
	}
	return UserSocketPath()
}
