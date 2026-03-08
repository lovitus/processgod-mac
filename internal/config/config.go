package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	appName = "ProcessGodMac"
)

// Config is the root config model.
type Config struct {
	PathEnv string `json:"pathEnv,omitempty"`
	Items   []Item `json:"items"`
}

// Item defines a single guarded process.
// It keeps legacy JSON keys for easier migration from the Windows version.
type Item struct {
	ID                 string            `json:"id,omitempty"`
	ProcessName        string            `json:"processName,omitempty"`
	ExecPath           string            `json:"execPath,omitempty"`
	EXEFullPath        string            `json:"EXEFullPath,omitempty"`
	StartupParams      string            `json:"startupParams,omitempty"`
	Args               []string          `json:"args,omitempty"`
	WorkingDir         string            `json:"workingDir,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
	OnlyOpenOnce       bool              `json:"onlyOpenOnce,omitempty"`
	Minimize           bool              `json:"minimize,omitempty"`
	NoWindow           bool              `json:"noWindow,omitempty"`
	Started            bool              `json:"started"`
	CronExpression     string            `json:"cronExpression,omitempty"`
	StopBeforeCronExec bool              `json:"stopBeforeCronExec,omitempty"`
}

func AppSupportDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("PROCESSGOD_HOME")); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", appName), nil
}

func EnsureAppSupportDir() (string, error) {
	dir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create app support dir: %w", err)
	}
	return dir, nil
}

func ConfigPath() (string, error) {
	dir, err := EnsureAppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func ControlAddress() string {
	if override := strings.TrimSpace(os.Getenv("PROCESSGOD_ADDR")); override != "" {
		return override
	}
	return "127.0.0.1:51089"
}

func EnsureDefaultConfig() (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat config: %w", err)
	}

	cfg := Config{PathEnv: DefaultPathEnv(), Items: []Item{}}
	if err := Save(path, cfg); err != nil {
		return "", err
	}
	return path, nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return Config{Items: []Item{}}, nil
	}

	// Accept legacy top-level array.
	if strings.HasPrefix(trimmed, "[") {
		var items []Item
		if err := json.Unmarshal(data, &items); err != nil {
			return Config{}, fmt.Errorf("decode legacy array config: %w", err)
		}
		cfg := Config{Items: items}
		cfg.Normalize()
		return cfg, nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	cfg.Normalize()
	return cfg, nil
}

func Save(path string, cfg Config) error {
	cfg.Normalize()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (c *Config) Normalize() {
	c.PathEnv = strings.TrimSpace(c.PathEnv)
	if c.PathEnv == "" {
		c.PathEnv = DefaultPathEnv()
	}
	for i := range c.Items {
		c.Items[i].Normalize()
	}
}

// DefaultPathEnv returns the PATH used by daemon and child processes.
func DefaultPathEnv() string {
	if v := strings.TrimSpace(os.Getenv("PATH")); v != "" {
		return v
	}
	return "/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/System/Cryptexes/App/usr/bin:/usr/bin:/bin:/usr/sbin:/sbin:/var/run/com.apple.security.cryptexd/codex.system/bootstrap/usr/local/bin:/var/run/com.apple.security.cryptexd/codex.system/bootstrap/usr/bin:/var/run/com.apple.security.cryptexd/codex.system/bootstrap/usr/appleinternal/bin:/Users/fanli/.cargo/bin"
}

func (i *Item) Normalize() {
	i.ID = strings.TrimSpace(i.ID)
	i.ProcessName = strings.TrimSpace(i.ProcessName)
	i.ExecPath = strings.TrimSpace(i.ExecPath)
	i.EXEFullPath = strings.TrimSpace(i.EXEFullPath)
	i.StartupParams = strings.TrimSpace(i.StartupParams)
	i.WorkingDir = strings.TrimSpace(i.WorkingDir)
	i.CronExpression = strings.TrimSpace(i.CronExpression)

	if i.ExecPath == "" {
		i.ExecPath = i.EXEFullPath
	}
	if i.EXEFullPath == "" {
		i.EXEFullPath = i.ExecPath
	}

	if i.ID == "" {
		base := filepath.Base(i.ExecPath)
		if base == "" || base == "." || base == string(filepath.Separator) {
			base = "task"
		}
		i.ID = sanitizeID(strings.TrimSuffix(base, filepath.Ext(base)))
	}

	if i.ProcessName == "" {
		i.ProcessName = filepath.Base(i.ExecPath)
	}

	if len(i.Args) == 0 && i.StartupParams != "" {
		i.Args = SplitArgs(i.StartupParams)
	}

	if i.WorkingDir == "" && i.ExecPath != "" {
		i.WorkingDir = filepath.Dir(i.ExecPath)
	}
}

func Validate(cfg Config) error {
	seen := make(map[string]struct{}, len(cfg.Items))
	for idx := range cfg.Items {
		it := cfg.Items[idx]
		if strings.TrimSpace(it.ID) == "" {
			return fmt.Errorf("items[%d]: id is required", idx)
		}
		if _, ok := seen[it.ID]; ok {
			return fmt.Errorf("items[%d]: duplicate id %q", idx, it.ID)
		}
		seen[it.ID] = struct{}{}
		if it.Started {
			if strings.TrimSpace(it.ExecPath) == "" {
				return fmt.Errorf("items[%d]: execPath is required when started=true", idx)
			}
		}
	}
	return nil
}

func sanitizeID(s string) string {
	if s == "" {
		return "task"
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return "task"
	}
	return strings.ToLower(out)
}

// SplitArgs parses shell-like arguments with support for basic single/double quotes and escapes.
func SplitArgs(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}

	var args []string
	var cur strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	flush := func() {
		if cur.Len() == 0 {
			return
		}
		args = append(args, cur.String())
		cur.Reset()
	}

	for _, r := range input {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && !inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case unicode.IsSpace(r) && !inSingle && !inDouble:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()

	return args
}
