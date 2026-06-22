package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/lovitus/processgod-mac/internal/api"
	"github.com/lovitus/processgod-mac/internal/config"
	"github.com/lovitus/processgod-mac/internal/cron"
	"github.com/lovitus/processgod-mac/internal/guardian"
	"github.com/lovitus/processgod-mac/internal/ipc"
)

type Daemon struct {
	configPath string
	socketPath string
	scope      ipc.Scope
	manager    *guardian.Manager
	store      *config.Store
	events     *ipc.EventHub
	logger     *log.Logger

	mu        sync.RWMutex
	healthErr error
	ownerUID  uint32
	stopOnce  sync.Once
	stopFunc  func()
}

type stateFile struct {
	OwnerUID uint32 `json:"ownerUID"`
}

func NewDaemon(configPath, socketPath string, scope ipc.Scope) *Daemon {
	logger := log.New(os.Stdout, "[processgod] ", log.LstdFlags)
	daemon := &Daemon{
		configPath: configPath,
		socketPath: socketPath,
		scope:      scope,
		manager:    guardian.New(logger),
		store:      config.OpenStore(configPath),
		events:     ipc.NewEventHub(),
		logger:     logger,
	}
	if scope == ipc.ScopeSystem {
		daemon.ownerUID = daemon.loadOwnerUID()
	} else {
		daemon.ownerUID = uint32(os.Getuid())
	}
	daemon.manager.SetNotifier(func(eventType, processID string) {
		snapshot, _ := daemon.store.APISnapshot()
		daemon.events.Publish(eventType, processID, snapshot.Revision)
	})
	return daemon
}

func (d *Daemon) SetStopFunc(fn func()) { d.stopFunc = fn }

func (d *Daemon) Run(stop <-chan struct{}) error {
	cfg, loadErr := d.store.Snapshot()
	if loadErr == nil {
		if err := config.Validate(cfg); err != nil {
			d.setHealthError(err)
		} else if err := d.manager.Apply(cfg); err != nil {
			d.setHealthError(err)
		}
	} else {
		d.setHealthError(loadErr)
	}

	server := &ipc.Server{SocketPath: d.socketPath, Scope: d.scope, Handler: d}
	managerStop := make(chan struct{})
	var shutdownOnce sync.Once
	shutdown := func() { shutdownOnce.Do(func() { close(managerStop) }) }
	go func() {
		select {
		case <-stop:
			shutdown()
		case <-managerStop:
		}
	}()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.manager.Run(managerStop)
	}()
	err := server.Run(managerStop)
	shutdown()
	wg.Wait()
	return err
}

func (d *Daemon) Handle(peer ipc.Peer, request ipc.Request) ipc.Response {
	if rpcErr := d.authorize(peer, request.Method); rpcErr != nil {
		return ipc.ErrorResponse(request.RequestID, rpcErr.Code, rpcErr.Message, rpcErr.Details)
	}
	switch request.Method {
	case "system.hello":
		return ipc.SuccessResponse(request.RequestID, map[string]any{
			"protocolVersion": api.ProtocolVersion,
			"version":         version,
			"mode":            d.scope,
			"capabilities":    []string{"configCRUD", "structuredLogs", "events", "pause", "cronValidation"},
		})
	case "system.health":
		return ipc.SuccessResponse(request.RequestID, d.runtimeSnapshot())
	case "system.bootstrap":
		return d.handleBootstrap(peer, request)
	case "config.get":
		snapshot, _ := d.store.APISnapshot()
		return ipc.SuccessResponse(request.RequestID, snapshot)
	case "config.export":
		cfg, _ := d.store.Snapshot()
		return ipc.SuccessResponse(request.RequestID, cfg)
	case "config.import":
		var params struct {
			ExpectedRevision uint64          `json:"expectedRevision"`
			Config           json.RawMessage `json:"config"`
		}
		if err := decodeParams(request, &params); err != nil {
			return rpcFailure(request, err)
		}
		cfg, err := d.importConfig(params.Config, params.ExpectedRevision)
		return d.afterConfigMutation(request, cfg, err)
	case "config.reload":
		cfg, err := d.store.Reload()
		return d.afterConfigMutation(request, cfg, err)
	case "process.create":
		return d.createProcess(request)
	case "process.update":
		return d.updateProcess(request)
	case "process.delete":
		return d.deleteProcess(request)
	case "process.setEnabled":
		return d.setProcessEnabled(request)
	case "process.restart":
		return d.restartProcess(request)
	case "guardian.pause":
		return d.setPaused(request, true)
	case "guardian.resume":
		return d.setPaused(request, false)
	case "settings.update":
		return d.updateSettings(request)
	case "status.list":
		return ipc.SuccessResponse(request.RequestID, d.runtimeSnapshot())
	case "logs.snapshot":
		var params struct {
			ID    string `json:"id"`
			Lines int    `json:"lines,omitempty"`
		}
		if err := decodeParams(request, &params); err != nil {
			return rpcFailure(request, err)
		}
		snapshot, err := d.manager.LogSnapshot(params.ID, params.Lines)
		if err != nil {
			return rpcFailure(request, err)
		}
		return ipc.SuccessResponse(request.RequestID, snapshot)
	case "cron.validate":
		var params struct {
			Expression string `json:"expression"`
		}
		if err := decodeParams(request, &params); err != nil {
			return rpcFailure(request, err)
		}
		if _, err := cron.Parse(params.Expression); err != nil {
			return ipc.ErrorResponse(request.RequestID, "invalid_cron", err.Error(), nil)
		}
		return ipc.SuccessResponse(request.RequestID, map[string]bool{"valid": true})
	case "daemon.shutdown":
		if d.stopFunc == nil {
			return ipc.ErrorResponse(request.RequestID, "invalid_state", "stop function is not configured", nil)
		}
		d.stopOnce.Do(d.stopFunc)
		return ipc.SuccessResponse(request.RequestID, map[string]bool{"stopping": true})
	default:
		return ipc.ErrorResponse(request.RequestID, "method_not_found", "unknown method", map[string]any{"method": request.Method})
	}
}

func (d *Daemon) Subscribe(peer ipc.Peer) (<-chan api.Event, func(), *ipc.RPCError) {
	if rpcErr := d.authorize(peer, "events.subscribe"); rpcErr != nil {
		return nil, func() {}, rpcErr
	}
	events, cancel := d.events.Subscribe()
	return events, cancel, nil
}

func (d *Daemon) createProcess(request ipc.Request) ipc.Response {
	var params struct {
		ExpectedRevision uint64                `json:"expectedRevision"`
		Process          api.ProcessDefinition `json:"process"`
	}
	if err := decodeParams(request, &params); err != nil {
		return rpcFailure(request, err)
	}
	item, err := config.ItemFromDefinition(params.Process)
	if err != nil {
		return rpcFailure(request, err)
	}
	cfg, err := d.store.Update(params.ExpectedRevision, func(cfg *config.Config) error {
		for _, existing := range cfg.Items {
			if existing.ID == item.ID {
				return fmt.Errorf("process id %q already exists", item.ID)
			}
		}
		cfg.Items = append(cfg.Items, item)
		return nil
	})
	return d.afterConfigMutation(request, cfg, err)
}

func (d *Daemon) updateProcess(request ipc.Request) ipc.Response {
	var params struct {
		ExpectedRevision uint64                `json:"expectedRevision"`
		ID               string                `json:"id"`
		Process          api.ProcessDefinition `json:"process"`
	}
	if err := decodeParams(request, &params); err != nil {
		return rpcFailure(request, err)
	}
	item, err := config.ItemFromDefinition(params.Process)
	if err != nil {
		return rpcFailure(request, err)
	}
	cfg, err := d.store.Update(params.ExpectedRevision, func(cfg *config.Config) error {
		index := -1
		for i, existing := range cfg.Items {
			if existing.ID == params.ID {
				index = i
				item.Minimize = existing.Minimize
				item.NoWindow = existing.NoWindow
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("process id %q not found", params.ID)
		}
		cfg.Items[index] = item
		return nil
	})
	return d.afterConfigMutation(request, cfg, err)
}

func (d *Daemon) deleteProcess(request ipc.Request) ipc.Response {
	var params idRevisionParams
	if err := decodeParams(request, &params); err != nil {
		return rpcFailure(request, err)
	}
	cfg, err := d.store.Update(params.ExpectedRevision, func(cfg *config.Config) error {
		items := make([]config.Item, 0, len(cfg.Items))
		found := false
		for _, item := range cfg.Items {
			if item.ID == params.ID {
				found = true
				continue
			}
			items = append(items, item)
		}
		if !found {
			return fmt.Errorf("process id %q not found", params.ID)
		}
		cfg.Items = items
		return nil
	})
	return d.afterConfigMutation(request, cfg, err)
}

func (d *Daemon) setProcessEnabled(request ipc.Request) ipc.Response {
	var params struct {
		ID               string `json:"id"`
		Enabled          bool   `json:"enabled"`
		ExpectedRevision uint64 `json:"expectedRevision"`
	}
	if err := decodeParams(request, &params); err != nil {
		return rpcFailure(request, err)
	}
	cfg, err := d.store.Update(params.ExpectedRevision, func(cfg *config.Config) error {
		for i := range cfg.Items {
			if cfg.Items[i].ID == params.ID {
				cfg.Items[i].Started = params.Enabled
				return nil
			}
		}
		return fmt.Errorf("process id %q not found", params.ID)
	})
	return d.afterConfigMutation(request, cfg, err)
}

func (d *Daemon) restartProcess(request ipc.Request) ipc.Response {
	var params struct {
		ID string `json:"id"`
	}
	if err := decodeParams(request, &params); err != nil {
		return rpcFailure(request, err)
	}
	if err := d.manager.Restart(params.ID); err != nil {
		return rpcFailure(request, err)
	}
	return ipc.SuccessResponse(request.RequestID, d.runtimeSnapshot())
}

func (d *Daemon) setPaused(request ipc.Request, paused bool) ipc.Response {
	var params struct {
		ExpectedRevision uint64 `json:"expectedRevision"`
	}
	if len(request.Params) > 0 {
		if err := decodeParams(request, &params); err != nil {
			return rpcFailure(request, err)
		}
	}
	cfg, err := d.store.Update(params.ExpectedRevision, func(cfg *config.Config) error {
		cfg.GuardianPaused = paused
		return nil
	})
	return d.afterConfigMutation(request, cfg, err)
}

func (d *Daemon) updateSettings(request ipc.Request) ipc.Response {
	var params struct {
		ExpectedRevision uint64 `json:"expectedRevision"`
		PathEnv          string `json:"pathEnv"`
	}
	if err := decodeParams(request, &params); err != nil {
		return rpcFailure(request, err)
	}
	cfg, err := d.store.Update(params.ExpectedRevision, func(cfg *config.Config) error {
		cfg.PathEnv = strings.TrimSpace(params.PathEnv)
		return nil
	})
	return d.afterConfigMutation(request, cfg, err)
}

func (d *Daemon) afterConfigMutation(request ipc.Request, cfg config.Config, err error) ipc.Response {
	if err != nil {
		return rpcFailure(request, err)
	}
	if err := d.manager.Apply(cfg); err != nil {
		d.setHealthError(err)
		return rpcFailure(request, err)
	}
	d.setHealthError(nil)
	d.events.Publish("config.changed", "", cfg.Revision)
	return ipc.SuccessResponse(request.RequestID, cfg.Snapshot())
}

func (d *Daemon) runtimeSnapshot() api.RuntimeSnapshot {
	cfg, loadErr := d.store.Snapshot()
	statuses := d.manager.Statuses()
	statusByID := make(map[string]guardian.Status, len(statuses))
	for _, status := range statuses {
		statusByID[status.ID] = status
	}
	processes := make([]api.ProcessRuntime, 0, len(cfg.Items))
	for _, item := range cfg.Items {
		status, found := statusByID[item.ID]
		runtime := api.ProcessRuntime{ID: item.ID, Name: item.ProcessName, State: api.StateWaiting}
		switch {
		case !item.Started:
			runtime.State = api.StateDisabled
		case found && status.Running:
			runtime.State = api.StateRunning
			runtime.PID = status.PID
		case found && status.LastError != "":
			runtime.State = api.StateError
			runtime.ErrorCode = "process_error"
			runtime.Error = status.LastError
		case found && item.OnlyOpenOnce && !status.LastExit.IsZero():
			runtime.State = api.StateCompleted
		}
		if found {
			if !status.LastStart.IsZero() {
				value := status.LastStart.UTC()
				runtime.LastStart = &value
			}
			if !status.LastExit.IsZero() {
				value := status.LastExit.UTC()
				runtime.LastExit = &value
			}
		}
		processes = append(processes, runtime)
	}
	healthErr := d.getHealthError()
	if loadErr != nil {
		healthErr = loadErr
	}
	result := api.RuntimeSnapshot{Mode: string(d.scope), Paused: cfg.GuardianPaused, Healthy: healthErr == nil, Processes: processes}
	if healthErr != nil {
		result.Error = healthErr.Error()
	}
	return result
}

func (d *Daemon) handleBootstrap(peer ipc.Peer, request ipc.Request) ipc.Response {
	if d.scope != ipc.ScopeSystem {
		return ipc.ErrorResponse(request.RequestID, "invalid_scope", "bootstrap is only valid for system mode", nil)
	}
	if d.ownerUID != 0 {
		return ipc.ErrorResponse(request.RequestID, "already_bootstrapped", "system daemon already has an owner", nil)
	}
	if !isAdminUID(peer.UID) {
		return ipc.ErrorResponse(request.RequestID, "permission_denied", "system bootstrap requires an administrator account", nil)
	}
	var params struct {
		Config json.RawMessage `json:"config"`
	}
	if err := decodeParams(request, &params); err != nil {
		return rpcFailure(request, err)
	}
	cfg, err := d.importConfig(params.Config, 0)
	if err != nil {
		return rpcFailure(request, err)
	}
	if err := d.saveOwnerUID(peer.UID); err != nil {
		return rpcFailure(request, err)
	}
	d.ownerUID = peer.UID
	return d.afterConfigMutation(request, cfg, nil)
}

func (d *Daemon) importConfig(raw json.RawMessage, expectedRevision uint64) (config.Config, error) {
	var legacyItems []config.Item
	if err := json.Unmarshal(raw, &legacyItems); err == nil && legacyItems != nil {
		return d.store.ReplaceConfig(config.Config{Items: legacyItems}, expectedRevision)
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(raw, &shape); err != nil {
		return config.Config{}, fmt.Errorf("invalid imported config: %w", err)
	}
	if _, isStorageConfig := shape["items"]; isStorageConfig {
		var imported config.Config
		if err := json.Unmarshal(raw, &imported); err != nil {
			return config.Config{}, fmt.Errorf("invalid storage config: %w", err)
		}
		return d.store.ReplaceConfig(imported, expectedRevision)
	}
	var snapshot api.ConfigSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return config.Config{}, fmt.Errorf("invalid API config: %w", err)
	}
	return d.store.Replace(snapshot, expectedRevision)
}

func (d *Daemon) authorize(peer ipc.Peer, method string) *ipc.RPCError {
	if d.scope != ipc.ScopeSystem {
		return nil
	}
	if d.ownerUID == 0 {
		if method == "system.hello" || method == "system.health" || method == "system.bootstrap" {
			return nil
		}
		return &ipc.RPCError{Code: "not_bootstrapped", Message: "system daemon has no owner"}
	}
	if peer.UID != 0 && peer.UID != d.ownerUID {
		return &ipc.RPCError{Code: "permission_denied", Message: "client does not own this system daemon"}
	}
	return nil
}

func (d *Daemon) statePath() string { return filepath.Join(filepath.Dir(d.configPath), "state.json") }

func (d *Daemon) loadOwnerUID() uint32 {
	data, err := os.ReadFile(d.statePath())
	if err != nil {
		return 0
	}
	var state stateFile
	if json.Unmarshal(data, &state) != nil {
		return 0
	}
	return state.OwnerUID
}

func (d *Daemon) saveOwnerUID(uid uint32) error {
	data, _ := json.MarshalIndent(stateFile{OwnerUID: uid}, "", "  ")
	dirPath := filepath.Dir(d.statePath())
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dirPath, ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, d.statePath()); err != nil {
		return err
	}
	if dir, err := os.Open(dirPath); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (d *Daemon) setHealthError(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.healthErr = err
}

func (d *Daemon) getHealthError() error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.healthErr
}

type idRevisionParams struct {
	ID               string `json:"id"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}

func decodeParams(request ipc.Request, value any) error {
	if len(request.Params) == 0 {
		return errors.New("request params are required")
	}
	if err := json.Unmarshal(request.Params, value); err != nil {
		return fmt.Errorf("invalid request params: %w", err)
	}
	return nil
}

func rpcFailure(request ipc.Request, err error) ipc.Response {
	code := "invalid_request"
	details := map[string]any(nil)
	if errors.Is(err, config.ErrRevisionConflict) {
		code = "revision_conflict"
		if cfgErr, ok := err.(interface{ Unwrap() error }); ok && cfgErr.Unwrap() != nil {
			details = map[string]any{"reason": err.Error()}
		}
	} else if strings.Contains(err.Error(), "not found") {
		code = "not_found"
	} else if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "duplicate") {
		code = "duplicate_id"
	} else if strings.Contains(err.Error(), "cron") {
		code = "invalid_cron"
	} else if errors.Is(err, config.ErrRevisionExhausted) {
		code = "revision_exhausted"
	}
	return ipc.ErrorResponse(request.RequestID, code, err.Error(), details)
}

func isAdminUID(uid uint32) bool {
	account, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return false
	}
	group, err := user.LookupGroup("admin")
	if err != nil {
		return false
	}
	groups, err := account.GroupIds()
	if err != nil {
		return false
	}
	for _, id := range groups {
		if id == group.Gid {
			return true
		}
	}
	return false
}

var version = "0.4.0-dev"
