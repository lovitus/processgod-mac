package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/processgod-mac/internal/api"
	"github.com/lovitus/processgod-mac/internal/config"
	"github.com/lovitus/processgod-mac/internal/ipc"
)

func TestDaemonConfigCRUDPauseAndRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	daemon := NewDaemon(path, filepath.Join(t.TempDir(), "control.sock"), ipc.ScopeUser)
	peer := ipc.Peer{UID: uint32(os.Getuid())}

	initial := callDaemon[api.ConfigSnapshot](t, daemon, peer, "config.get", nil)
	created := callDaemon[api.ConfigSnapshot](t, daemon, peer, "process.create", map[string]any{
		"expectedRevision": initial.Revision,
		"process":          api.ProcessDefinition{ID: "echo", Name: "Echo", Command: "/bin/echo", Arguments: "hello", Mode: api.ModeGuard, Enabled: true},
	})
	if created.Revision != initial.Revision+1 || len(created.Processes) != 1 {
		t.Fatalf("unexpected create result: %+v", created)
	}

	conflict := rawCall(t, daemon, peer, "process.setEnabled", map[string]any{"id": "echo", "enabled": false, "expectedRevision": initial.Revision})
	if conflict.OK || conflict.Error == nil || conflict.Error.Code != "revision_conflict" {
		t.Fatalf("expected revision conflict: %+v", conflict)
	}

	paused := callDaemon[api.ConfigSnapshot](t, daemon, peer, "guardian.pause", map[string]any{"expectedRevision": created.Revision})
	if !paused.GuardianPaused {
		t.Fatalf("guardian pause was not persisted: %+v", paused)
	}
	runtime := callDaemon[api.RuntimeSnapshot](t, daemon, peer, "status.list", nil)
	if !runtime.Paused || len(runtime.Processes) != 1 {
		t.Fatalf("unexpected runtime snapshot: %+v", runtime)
	}
}

func TestDaemonRunReturnsWhenSocketListenFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	socket := filepath.Join(t.TempDir(), strings.Repeat("socket-path-", 20), "control.sock")
	daemon := NewDaemon(path, socket, ipc.ScopeUser)
	done := make(chan error, 1)
	go func() { done <- daemon.Run(make(chan struct{})) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected socket listen failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop after socket listen failure")
	}
}

func TestSystemBootstrapAndOwnerAuthorization(t *testing.T) {
	uid := uint32(os.Getuid())
	if !isAdminUID(uid) {
		t.Skip("test account is not a local administrator")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	daemon := NewDaemon(path, filepath.Join(t.TempDir(), "system.sock"), ipc.ScopeSystem)
	owner := ipc.Peer{UID: uid}

	before := rawCall(t, daemon, owner, "config.get", nil)
	if before.OK || before.Error == nil || before.Error.Code != "not_bootstrapped" {
		t.Fatalf("expected not_bootstrapped, got %+v", before)
	}
	storage := config.Config{Items: []config.Item{{ID: "legacy", ProcessName: "Legacy", ExecPath: "/bin/echo", Minimize: true, Started: false}}}
	bootstrapped := callDaemon[api.ConfigSnapshot](t, daemon, owner, "system.bootstrap", map[string]any{"config": storage})
	if len(bootstrapped.Processes) != 1 {
		t.Fatalf("unexpected bootstrap result: %+v", bootstrapped)
	}
	loaded, err := config.Load(path)
	if err != nil || !loaded.Items[0].Minimize {
		t.Fatalf("bootstrap lost storage fields: config=%+v err=%v", loaded, err)
	}

	other := rawCall(t, daemon, ipc.Peer{UID: uid + 100_000}, "config.get", nil)
	if other.OK || other.Error == nil || other.Error.Code != "permission_denied" {
		t.Fatalf("expected owner authorization failure, got %+v", other)
	}
}

func TestConfigImportAcceptsLegacyTopLevelArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	daemon := NewDaemon(path, filepath.Join(t.TempDir(), "control.sock"), ipc.ScopeUser)
	legacy := []config.Item{{ID: "old", ProcessName: "Old", ExecPath: "/bin/echo", Minimize: true, NoWindow: true}}
	result := callDaemon[api.ConfigSnapshot](t, daemon, ipc.Peer{UID: uint32(os.Getuid())}, "config.import", map[string]any{
		"expectedRevision": uint64(1),
		"config":           legacy,
	})
	if len(result.Processes) != 1 || result.Processes[0].ID != "old" {
		t.Fatalf("unexpected import result: %+v", result)
	}
	loaded, err := config.Load(path)
	if err != nil || !loaded.Items[0].Minimize || !loaded.Items[0].NoWindow {
		t.Fatalf("legacy fields were not retained: config=%+v err=%v", loaded, err)
	}
}

func TestConfigReloadUpdatesStoreAndRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	daemon := NewDaemon(path, filepath.Join(t.TempDir(), "control.sock"), ipc.ScopeUser)
	peer := ipc.Peer{UID: uint32(os.Getuid())}
	initial := callDaemon[api.ConfigSnapshot](t, daemon, peer, "config.get", nil)
	manual := config.Config{Revision: initial.Revision, PathEnv: "/manual/bin", Items: []config.Item{}}
	if err := config.Save(path, manual); err != nil {
		t.Fatalf("save manual edit: %v", err)
	}
	reloaded := callDaemon[api.ConfigSnapshot](t, daemon, peer, "config.reload", nil)
	if reloaded.PathEnv != "/manual/bin" || reloaded.Revision != initial.Revision+1 {
		t.Fatalf("unexpected reload result: %+v", reloaded)
	}
	current := callDaemon[api.ConfigSnapshot](t, daemon, peer, "config.get", nil)
	if current.PathEnv != reloaded.PathEnv || current.Revision != reloaded.Revision {
		t.Fatalf("store did not retain reload: %+v", current)
	}
}

func rawCall(t *testing.T, daemon *Daemon, peer ipc.Peer, method string, params any) ipc.Response {
	t.Helper()
	request := ipc.Request{ProtocolVersion: api.ProtocolVersion, RequestID: method, Method: method}
	if params != nil {
		request.Params, _ = json.Marshal(params)
	}
	return daemon.Handle(peer, request)
}

func callDaemon[T any](t *testing.T, daemon *Daemon, peer ipc.Peer, method string, params any) T {
	t.Helper()
	response := rawCall(t, daemon, peer, method, params)
	if !response.OK {
		t.Fatalf("%s failed: %+v", method, response.Error)
	}
	var result T
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("decode %s result: %v", method, err)
	}
	return result
}
