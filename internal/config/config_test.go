package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lovitus/processgod-mac/internal/api"
)

func TestSplitArgs(t *testing.T) {
	args := SplitArgs(`--name "hello world" --path '/tmp/a b' plain`)
	want := []string{"--name", "hello world", "--path", "/tmp/a b", "plain"}

	if len(args) != len(want) {
		t.Fatalf("arg count mismatch: want %d got %d (%v)", len(want), len(args), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d]: want %q got %q", i, want[i], args[i])
		}
	}
}

func TestStoreRevisionAndAtomicSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Config{}); err != nil {
		t.Fatalf("save initial config: %v", err)
	}
	store := OpenStore(path)
	initial, err := store.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	updated, err := store.Update(initial.Revision, func(cfg *Config) error {
		cfg.PathEnv = "/bin"
		return nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Revision != initial.Revision+1 {
		t.Fatalf("revision did not increase: %d -> %d", initial.Revision, updated.Revision)
	}
	_, err = store.Update(initial.Revision, func(*Config) error { return nil })
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "config.json" {
			t.Fatalf("temporary file was left behind: %s", entry.Name())
		}
	}
}

func TestStoreRejectsRevisionOverflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Config{Revision: ^uint64(0)}); err != nil {
		t.Fatalf("save initial config: %v", err)
	}
	store := OpenStore(path)
	_, err := store.Update(^uint64(0), func(*Config) error { return nil })
	if !errors.Is(err, ErrRevisionExhausted) {
		t.Fatalf("expected revision exhaustion, got %v", err)
	}
}

func TestReplaceConfigPreservesCompatibilityFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Config{}); err != nil {
		t.Fatalf("save initial config: %v", err)
	}
	store := OpenStore(path)
	imported := Config{Items: []Item{{ID: "legacy", ProcessName: "Legacy", ExecPath: "/bin/echo", Minimize: true, NoWindow: true}}}
	result, err := store.ReplaceConfig(imported, 1)
	if err != nil {
		t.Fatalf("replace config: %v", err)
	}
	if !result.Items[0].Minimize || !result.Items[0].NoWindow {
		t.Fatalf("compatibility fields were lost: %+v", result.Items[0])
	}
}

func TestLegacyConfigMigratesToAPISnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `[{"id":"job","processName":"Job","EXEFullPath":"/bin/echo","startupParams":"hello","started":true,"onlyOpenOnce":true}]`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}
	snapshot := cfg.Snapshot()
	if snapshot.SchemaVersion != CurrentSchemaVersion || snapshot.Revision != 1 {
		t.Fatalf("legacy metadata not normalized: %+v", snapshot)
	}
	if len(snapshot.Processes) != 1 || snapshot.Processes[0].Mode != api.ModeOnce || snapshot.Processes[0].Command != "/bin/echo" {
		t.Fatalf("unexpected process migration: %+v", snapshot.Processes)
	}
}

func TestDefinitionModeRoundTrip(t *testing.T) {
	definition := api.ProcessDefinition{
		ID: "cron", Name: "Cron", Command: "/bin/echo", Arguments: "hello world",
		Mode: api.ModeCronRestart, CronExpression: "*/5 * * * *", Enabled: true,
		Environment: map[string]string{"A": "B"},
	}
	item, err := ItemFromDefinition(definition)
	if err != nil {
		t.Fatalf("definition to item: %v", err)
	}
	result := item.Definition()
	if result.Mode != api.ModeCronRestart || result.CronExpression != definition.CronExpression || result.Environment["A"] != "B" {
		t.Fatalf("round trip mismatch: %+v", result)
	}
}

func TestNormalizeFillsLegacyFields(t *testing.T) {
	it := Item{ID: "a", EXEFullPath: "/bin/echo", StartupParams: "hello world"}
	it.Normalize()
	if it.ExecPath != "/bin/echo" {
		t.Fatalf("expected exec path from legacy field, got %q", it.ExecPath)
	}
	if len(it.Args) != 2 || it.Args[0] != "hello" || it.Args[1] != "world" {
		t.Fatalf("unexpected args: %#v", it.Args)
	}
}

func TestValidateRejectsFutureSchemaAndInvalidCron(t *testing.T) {
	if err := Validate(Config{SchemaVersion: CurrentSchemaVersion + 1}); err == nil {
		t.Fatal("expected future schema rejection")
	}
	invalid := Config{Items: []Item{{ID: "cron", ExecPath: "/bin/echo", CronExpression: "not cron", Started: true}}}
	if err := Validate(invalid); err == nil {
		t.Fatal("expected invalid cron rejection")
	}
}
