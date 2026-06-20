package dashboard

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/lovitus/processgod-mac/internal/config"
	"github.com/lovitus/processgod-mac/internal/daemonctl"
	"github.com/lovitus/processgod-mac/internal/guardian"
	"github.com/lovitus/processgod-mac/internal/ipc"
)

type Server struct {
	Addr        string
	ConfigPath  string
	ControlAddr string
	ExePath     string
	WorkDir     string
}

type row struct {
	Item        config.Item
	Status      guardian.Status
	Has         bool
	ModeLabel   string
	ToggleLabel string
	StateLabel  string
	StateClass  string
}

func (s *Server) Run(openBrowser bool) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/action", s.handleAction)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/language", s.handleLanguage)

	if openBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			_ = exec.Command("open", "http://"+s.Addr).Run()
		}()
	}

	return http.ListenAndServe(s.Addr, mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	loc := localizerForRequest(r)
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		http.Error(w, "load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	editID := strings.TrimSpace(r.URL.Query().Get("edit"))
	showEditor := strings.TrimSpace(r.URL.Query().Get("new")) == "1"
	edit := config.Item{Started: true, StopBeforeCronExec: true, NoWindow: true}
	editFound := false
	if editID != "" {
		for _, it := range cfg.Items {
			if it.ID == editID {
				edit = it
				editFound = true
				showEditor = true
				break
			}
		}
	}

	statuses, online, statusErr, level, _ := queryStatus(s.ControlAddr)
	statusByID := make(map[string]guardian.Status, len(statuses))
	for _, st := range statuses {
		statusByID[st.ID] = st
	}

	sort.Slice(cfg.Items, func(i, j int) bool { return cfg.Items[i].ID < cfg.Items[j].ID })
	rows := make([]row, 0, len(cfg.Items))
	for _, it := range cfg.Items {
		st, ok := statusByID[it.ID]
		toggleLabel := loc.T("start")
		if it.Started {
			toggleLabel = loc.T("stop")
		}
		stateLabel, stateClass := itemState(it, st, ok, online, loc)
		rows = append(rows, row{
			Item:        it,
			Status:      st,
			Has:         ok,
			ModeLabel:   modeLabel(it, loc),
			ToggleLabel: toggleLabel,
			StateLabel:  stateLabel,
			StateClass:  stateClass,
		})
	}
	runningCount := 0
	enabledCount := 0
	for _, item := range cfg.Items {
		if item.Started {
			enabledCount++
		}
	}
	for _, st := range statuses {
		if st.Running {
			runningCount++
		}
	}

	startLabel := "Start Daemon (stopped)"
	stopLabel := "Stop Daemon (stopped)"
	startDisabled := false
	stopDisabled := true
	reloadDisabled := true
	if online {
		startLabel = "Start Daemon (running)"
		stopLabel = "Stop Daemon (running)"
		startDisabled = true
		stopDisabled = false
		reloadDisabled = false
	}

	flashErr := strings.TrimSpace(r.URL.Query().Get("error"))
	if !editFound && editID != "" {
		if flashErr == "" {
			flashErr = loc.Format("edit_target_not_found", editID)
		}
	}

	data := struct {
		Rows            []row
		Edit            config.Item
		EditFound       bool
		Online          bool
		StatusErr       string
		FlashErr        string
		FlashOK         string
		Addr            string
		CfgPath         string
		StartLabel      string
		StopLabel       string
		StartDisabled   bool
		StopDisabled    bool
		ReloadDisabled  bool
		PathEnv         string
		ServiceLevel    string
		ServiceHint     string
		ShowEditor      bool
		EditMode        string
		RunningCount    int
		EnabledCount    int
		ConfiguredCount int
		Lang            string
		L               localizer
		Summary         string
		ServiceMode     string
		CurrentURL      string
	}{
		Rows:            rows,
		Edit:            edit,
		EditFound:       editFound,
		Online:          online,
		StatusErr:       statusErr,
		FlashErr:        flashErr,
		FlashOK:         loc.T(strings.TrimSpace(r.URL.Query().Get("ok"))),
		Addr:            s.ControlAddr,
		CfgPath:         s.ConfigPath,
		StartLabel:      startLabel,
		StopLabel:       stopLabel,
		StartDisabled:   startDisabled,
		StopDisabled:    stopDisabled,
		ReloadDisabled:  reloadDisabled,
		PathEnv:         cfg.PathEnv,
		ServiceLevel:    level,
		ServiceHint:     localizedServiceHint(loc, level, online),
		ShowEditor:      showEditor,
		EditMode:        modeKey(edit),
		RunningCount:    runningCount,
		EnabledCount:    enabledCount,
		ConfiguredCount: len(cfg.Items),
		Lang:            loc.Lang(),
		L:               loc,
		Summary:         loc.Format("status_summary", runningCount, enabledCount, len(cfg.Items)),
		ServiceMode:     localizedServiceLevel(loc, level),
		CurrentURL:      r.URL.RequestURI(),
	}

	if err := managerPageTmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleLanguage(w http.ResponseWriter, r *http.Request) {
	lang := normalizeLanguage(r.URL.Query().Get("lang"))
	if lang == "" {
		lang = "en"
	}
	setLanguageCookie(w, lang)
	next := strings.TrimSpace(r.URL.Query().Get("next"))
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	lines := 200
	if n := strings.TrimSpace(r.URL.Query().Get("lines")); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			lines = v
		}
	}

	resp, err := ipc.Send(s.ControlAddr, ipc.Request{Action: "logs", ID: id, Lines: lines})
	if err != nil {
		http.Error(w, "daemon not reachable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	if !resp.OK {
		http.Error(w, resp.Error, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(resp.Logs))
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	action := strings.TrimSpace(r.FormValue("action"))
	var err error
	okMsg := "item_saved"

	switch action {
	case "start-daemon":
		err = daemonctl.EnsureRunning(s.ControlAddr, s.ExePath, s.WorkDir)
		okMsg = "daemon_started"
	case "stop-daemon":
		err = daemonctl.Stop(s.ControlAddr)
		okMsg = "daemon_stopped"
	case "reload":
		err = reloadIfRunning(s.ControlAddr)
		okMsg = "config_reloaded"
	case "toggle-item":
		err = s.toggleItem(strings.TrimSpace(r.FormValue("id")))
		okMsg = "item_toggled"
	case "restart-item":
		err = s.restartItem(strings.TrimSpace(r.FormValue("id")))
		okMsg = "process_restarted"
	case "delete-item":
		err = s.deleteItem(strings.TrimSpace(r.FormValue("id")))
		okMsg = "item_deleted"
	case "save-item":
		err = s.saveItem(r)
		okMsg = "item_saved"
	case "quick-add":
		err = s.quickAdd(r)
		okMsg = "item_added"
	case "save-path":
		err = s.savePath(strings.TrimSpace(r.FormValue("path_env")))
		okMsg = "path_updated"
	default:
		err = fmt.Errorf("unknown action: %s", action)
	}

	if err != nil {
		http.Redirect(w, r, "/?error="+urlEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?ok="+urlEscape(okMsg), http.StatusSeeOther)
}

func (s *Server) savePath(pathEnv string) error {
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		return err
	}
	if pathEnv == "" {
		pathEnv = config.DefaultPathEnv()
	}
	cfg.PathEnv = pathEnv
	if err := config.Save(s.ConfigPath, cfg); err != nil {
		return err
	}
	return reloadIfRunning(s.ControlAddr)
}

func (s *Server) toggleItem(id string) error {
	cfg, err := config.Load(s.ConfigPath)
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
		return fmt.Errorf("item %q not found", id)
	}
	if err := config.Save(s.ConfigPath, cfg); err != nil {
		return err
	}
	return reloadIfRunning(s.ControlAddr)
}

func (s *Server) deleteItem(id string) error {
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		return err
	}
	out := make([]config.Item, 0, len(cfg.Items))
	found := false
	for _, it := range cfg.Items {
		if it.ID == id {
			found = true
			continue
		}
		out = append(out, it)
	}
	if !found {
		return fmt.Errorf("item %q not found", id)
	}
	cfg.Items = out
	if err := config.Save(s.ConfigPath, cfg); err != nil {
		return err
	}
	return reloadIfRunning(s.ControlAddr)
}

func (s *Server) restartItem(id string) error {
	if id == "" {
		return errors.New("process id is required")
	}
	resp, err := ipc.Send(s.ControlAddr, ipc.Request{Action: "restart", ID: id})
	if err != nil {
		return err
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	return nil
}

func (s *Server) saveItem(r *http.Request) error {
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		return err
	}

	item := config.Item{
		ID:                 strings.TrimSpace(r.FormValue("id")),
		ProcessName:        strings.TrimSpace(r.FormValue("process_name")),
		ExecPath:           strings.TrimSpace(r.FormValue("exec_path")),
		StartupParams:      strings.TrimSpace(r.FormValue("startup_params")),
		WorkingDir:         strings.TrimSpace(r.FormValue("working_dir")),
		CronExpression:     strings.TrimSpace(r.FormValue("cron_expression")),
		Started:            r.FormValue("started") == "on",
		OnlyOpenOnce:       r.FormValue("only_open_once") == "on",
		Minimize:           r.FormValue("minimize") == "on",
		NoWindow:           r.FormValue("no_window") == "on",
		StopBeforeCronExec: r.FormValue("stop_before_cron_exec") == "on",
	}
	switch strings.TrimSpace(r.FormValue("mode")) {
	case "guard":
		item.OnlyOpenOnce = false
		item.CronExpression = ""
		item.StopBeforeCronExec = true
	case "once":
		item.OnlyOpenOnce = true
		item.CronExpression = ""
		item.StopBeforeCronExec = false
	case "cron-run":
		item.OnlyOpenOnce = false
		item.StopBeforeCronExec = false
	case "cron-restart":
		item.OnlyOpenOnce = false
		item.StopBeforeCronExec = true
	}
	if item.ID == "" {
		item.ID = slug(item.ProcessName)
	}
	if item.ID == "" {
		item.ID = slug(filepath.Base(item.ExecPath))
	}
	if item.ID == "" {
		return fmt.Errorf("id is required")
	}
	if item.ProcessName == "" {
		item.ProcessName = item.ID
	}

	origID := strings.TrimSpace(r.FormValue("original_id"))
	if origID == "" {
		origID = item.ID
	}

	replaced := false
	for i := range cfg.Items {
		if cfg.Items[i].ID == origID {
			item.Env = cfg.Items[i].Env
			cfg.Items[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Items = append(cfg.Items, item)
	}

	if err := config.Validate(cfg); err != nil {
		return err
	}
	if err := config.Save(s.ConfigPath, cfg); err != nil {
		return err
	}
	return reloadIfRunning(s.ControlAddr)
}

func (s *Server) quickAdd(r *http.Request) error {
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		return err
	}

	cmd := strings.TrimSpace(r.FormValue("quick_command"))
	if cmd == "" {
		return fmt.Errorf("command is required, e.g. ping or /usr/bin/python3")
	}
	name := strings.TrimSpace(r.FormValue("quick_name"))
	if name == "" {
		name = filepath.Base(cmd)
	}
	id := slug(name)
	if id == "" {
		id = slug(filepath.Base(cmd))
	}
	if id == "" {
		return fmt.Errorf("unable to generate id from command/name")
	}

	mode := strings.TrimSpace(r.FormValue("quick_mode"))
	cronExpr := strings.TrimSpace(r.FormValue("quick_cron"))
	if cronExpr == "" {
		cronExpr = "0 1 * * *"
	}

	item := config.Item{
		ID:            id,
		ProcessName:   name,
		ExecPath:      cmd,
		StartupParams: strings.TrimSpace(r.FormValue("quick_args")),
		Started:       true,
		NoWindow:      true,
	}
	item.Args = config.SplitArgs(item.StartupParams)

	switch mode {
	case "once":
		item.OnlyOpenOnce = true
		item.CronExpression = ""
		item.StopBeforeCronExec = false
	case "cron":
		item.OnlyOpenOnce = false
		item.CronExpression = cronExpr
		item.StopBeforeCronExec = true
	default:
		item.OnlyOpenOnce = false
		item.CronExpression = ""
		item.StopBeforeCronExec = true
	}

	for _, it := range cfg.Items {
		if it.ID == item.ID {
			return fmt.Errorf("id %q already exists; rename first", item.ID)
		}
	}
	cfg.Items = append(cfg.Items, item)

	if err := config.Validate(cfg); err != nil {
		return err
	}
	if err := config.Save(s.ConfigPath, cfg); err != nil {
		return err
	}
	return reloadIfRunning(s.ControlAddr)
}

func reloadIfRunning(controlAddr string) error {
	if !daemonctl.IsRunning(controlAddr) {
		return nil
	}
	resp, err := ipc.Send(controlAddr, ipc.Request{Action: "reload"})
	if err != nil {
		return err
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	return nil
}

func queryStatus(controlAddr string) ([]guardian.Status, bool, string, string, string) {
	resp, err := ipc.Send(controlAddr, ipc.Request{Action: "status"})
	if err != nil {
		return nil, false, err.Error(), "unknown", "Daemon unreachable."
	}
	if !resp.OK {
		return nil, false, resp.Error, "unknown", "Daemon status failed."
	}
	return resp.Status, true, "", resp.ServiceLevel, resp.ServiceHint
}

func modeLabel(it config.Item, loc localizer) string {
	if it.OnlyOpenOnce {
		return loc.T("mode_once")
	}
	if strings.TrimSpace(it.CronExpression) != "" {
		if it.StopBeforeCronExec {
			return loc.T("mode_cron_restart")
		}
		return loc.T("mode_cron_run")
	}
	return loc.T("mode_guard")
}

func modeKey(it config.Item) string {
	if it.OnlyOpenOnce {
		return "once"
	}
	if strings.TrimSpace(it.CronExpression) != "" {
		if it.StopBeforeCronExec {
			return "cron-restart"
		}
		return "cron-run"
	}
	return "guard"
}

func itemState(it config.Item, st guardian.Status, hasStatus, online bool, loc localizer) (string, string) {
	if !it.Started {
		return loc.T("state_disabled"), "disabled"
	}
	if !online {
		return loc.T("state_offline"), "offline"
	}
	if hasStatus && st.Running {
		return loc.Format("state_running", st.PID), "running"
	}
	if hasStatus && st.LastError != "" {
		return loc.Format("state_error", st.LastError), "error"
	}
	if hasStatus && it.OnlyOpenOnce && !st.LastExit.IsZero() {
		return loc.T("state_completed"), "disabled"
	}
	if hasStatus && strings.TrimSpace(it.CronExpression) != "" && !it.StopBeforeCronExec {
		return loc.T("state_wait_schedule"), "waiting"
	}
	return loc.T("state_wait_start"), "waiting"
}

func localizedServiceLevel(loc localizer, level string) string {
	switch level {
	case "system", "system-manual":
		return loc.T("service_system")
	case "user":
		return loc.T("service_user")
	case "manual":
		return loc.T("service_manual")
	default:
		return loc.T("service_unknown")
	}
}

func localizedServiceHint(loc localizer, level string, online bool) string {
	if !online {
		return loc.T("hint_offline")
	}
	switch level {
	case "system", "system-manual":
		return loc.T("hint_system")
	case "user":
		return loc.T("hint_user")
	default:
		return loc.T("hint_manual")
	}
}

func slug(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func urlEscape(s string) string {
	r := strings.NewReplacer(" ", "%20", "\n", "%0A", "\r", "", "#", "%23", "?", "%3F", "&", "%26")
	return r.Replace(s)
}

var managerPageTmpl = template.Must(template.New("manager").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>ProcessGodMac</title>
<style>
:root{
  --ink:#17201d;--muted:#66736d;--line:#dce4df;--paper:#fbfcfa;--panel:rgba(255,255,255,.86);
  --green:#147a55;--green-soft:#e3f3ea;--amber:#a15c09;--amber-soft:#fff0d5;--red:#b23b32;
  --shadow:0 18px 55px rgba(38,57,48,.11);--radius:18px
}
*{box-sizing:border-box}
body{margin:0;color:var(--ink);font-family:"Avenir Next","Helvetica Neue",sans-serif;background:
  radial-gradient(circle at 8% 0%,#dceee4 0,transparent 30%),
  radial-gradient(circle at 96% 8%,#f3e9cf 0,transparent 28%),#f3f5f1;min-height:100vh}
button,input,select{font:inherit}
button,a.button{border:0;border-radius:10px;padding:9px 13px;font-weight:650;cursor:pointer;text-decoration:none;display:inline-flex;align-items:center;justify-content:center;white-space:nowrap}
button{background:var(--ink);color:white}button:hover,a.button:hover{filter:brightness(.94)}button:disabled{opacity:.35;cursor:not-allowed}
.button.primary{background:var(--green);color:white}.button.ghost,button.ghost{background:#edf1ee;color:var(--ink)}
button.danger{background:#f7e7e4;color:var(--red)}a{color:var(--green);text-decoration:none}
.shell{width:min(1380px,calc(100% - 36px));margin:24px auto 44px}
.topbar{display:flex;gap:24px;align-items:center;justify-content:space-between;padding:18px 20px;background:var(--panel);border:1px solid rgba(255,255,255,.9);border-radius:var(--radius);box-shadow:var(--shadow);backdrop-filter:blur(18px)}
.brand{display:flex;align-items:center;gap:13px}.mark{width:42px;height:42px;border-radius:13px;background:var(--ink);color:white;display:grid;place-items:center;font-weight:800;letter-spacing:-1px}
.brand h1{font-size:20px;margin:0;letter-spacing:-.4px}.brand p{margin:2px 0 0;color:var(--muted);font-size:12px}
.daemon{display:flex;align-items:center;gap:11px}.daemon-copy{text-align:right}.daemon-copy strong{display:block;font-size:13px}.daemon-copy small{color:var(--muted)}
.lang-switch{display:flex;padding:3px;background:#edf1ee;border-radius:9px;margin-left:8px}.lang-switch a{font-size:11px;font-weight:750;color:var(--muted);padding:5px 8px;border-radius:7px}.lang-switch a.active{background:white;color:var(--ink);box-shadow:0 2px 8px rgba(30,49,40,.1)}
.dot{width:9px;height:9px;border-radius:50%;display:inline-block;background:#9aa39f;box-shadow:0 0 0 4px #edf0ee}.dot.online,.dot.running{background:#18a36e;box-shadow:0 0 0 4px #dff3e9}.dot.error{background:var(--red);box-shadow:0 0 0 4px #f7e4e1}.dot.waiting{background:#d9891c;box-shadow:0 0 0 4px #ffefd8}
.toolbar{display:flex;align-items:center;gap:8px;margin:18px 2px}.toolbar .spacer{flex:1}.summary{color:var(--muted);font-size:13px;margin-left:7px}
.flash{padding:10px 13px;border-radius:11px;margin:0 2px 14px;font-size:13px}.flash.ok{background:var(--green-soft);color:#0d6243}.flash.error{background:#f9e4e1;color:#8b2e27}
.layout{display:grid;grid-template-columns:minmax(0,1fr) 390px;gap:18px;align-items:start}
.panel{background:var(--panel);border:1px solid rgba(255,255,255,.95);border-radius:var(--radius);box-shadow:var(--shadow);overflow:hidden;backdrop-filter:blur(18px)}
.panel-head{padding:18px 20px 13px;display:flex;justify-content:space-between;align-items:end;border-bottom:1px solid var(--line)}.panel-head h2{margin:0;font-size:17px}.panel-head small{color:var(--muted)}
.tasks{padding:7px}.task{display:grid;grid-template-columns:minmax(210px,1.45fr) minmax(160px,1fr) 145px auto;gap:14px;align-items:center;padding:13px;border-radius:13px;border:1px solid transparent}.task:hover{background:#f4f7f4;border-color:#e5ebe7}
.task-main{min-width:0;display:grid;grid-template-columns:12px minmax(0,1fr);gap:10px;align-items:center}.task-name{font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.task-id,.command{color:var(--muted);font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.command{font-family:"SFMono-Regular",Menlo,monospace;font-size:11px}
.mode{font-size:11px;font-weight:700;color:#456157;background:#eaf0ec;border-radius:999px;padding:5px 9px;display:inline-block}.state{font-size:12px;min-width:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.state.error{color:var(--red)}.state.running{color:var(--green)}.state.waiting{color:var(--amber)}.state.disabled,.state.offline{color:var(--muted)}
.actions{display:flex;justify-content:flex-end;gap:5px}.actions button,.actions a{font-size:11px;padding:7px 9px}.inline{display:inline-flex;margin:0}
.empty{padding:64px 22px;text-align:center;color:var(--muted)}.empty strong{display:block;color:var(--ink);font-size:16px;margin-bottom:7px}
.inspector{position:sticky;top:18px}.inspector-head{padding:18px 20px 13px;border-bottom:1px solid var(--line)}.inspector-head h2{margin:0;font-size:17px}.inspector-head p{margin:4px 0 0;font-size:12px;color:var(--muted)}
.form-body{padding:18px 20px}.field{display:grid;gap:6px;margin-bottom:13px}.field label{font-size:11px;color:var(--muted);font-weight:750;text-transform:uppercase;letter-spacing:.55px}
input[type=text],select{width:100%;border:1px solid #ccd7d0;background:#fff;border-radius:10px;padding:10px 11px;color:var(--ink);outline:none}input:focus,select:focus{border-color:#529477;box-shadow:0 0 0 3px rgba(20,122,85,.11)}
.check{display:flex;align-items:center;gap:8px;font-size:13px;margin:9px 0}.check input{accent-color:var(--green)}
.advanced{border:1px solid var(--line);border-radius:12px;padding:0 12px;margin:15px 0}.advanced summary{cursor:pointer;padding:11px 0;font-size:12px;font-weight:700;color:#45534d}.advanced .inside{padding:2px 0 10px}
.form-actions{display:flex;gap:8px;padding-top:4px}.form-actions button{flex:1}.form-actions a{flex:0 0 auto}
.inspector-empty{padding:80px 28px;text-align:center;color:var(--muted)}.inspector-empty strong{display:block;color:var(--ink);font-size:17px;margin-bottom:7px}
.settings{margin:10px 7px 7px;border-top:1px solid var(--line);padding:3px 13px 12px}.settings summary{cursor:pointer;padding:13px 0;font-weight:700;font-size:12px;color:#4b5b54}.path-row{display:flex;gap:7px}.path-row input{font-family:"SFMono-Regular",Menlo,monospace;font-size:11px}.hint{font-size:11px;color:var(--muted);margin-top:7px;line-height:1.5}
@media(max-width:1020px){.layout{grid-template-columns:1fr}.inspector{position:static}.task{grid-template-columns:minmax(190px,1.3fr) minmax(140px,1fr) 130px auto}}
@media(max-width:760px){.shell{width:min(100% - 18px,680px);margin:10px auto 28px}.topbar{align-items:flex-start}.daemon-copy{display:none}.toolbar{flex-wrap:wrap}.toolbar .spacer{display:none}.summary{width:100%;margin:3px 0}.task{grid-template-columns:1fr;gap:7px;border-bottom:1px solid var(--line);border-radius:0}.actions{justify-content:flex-start;flex-wrap:wrap}.panel{border-radius:14px}.path-row{flex-wrap:wrap}}
</style>
<script>
function enablePathEdit(){
  const input=document.getElementById('path_env');
  input.dataset.original=input.value;input.readOnly=false;
  document.getElementById('path_modify').hidden=true;
  document.getElementById('path_save').hidden=false;
  document.getElementById('path_cancel').hidden=false;input.focus();
}
function cancelPathEdit(){
  const input=document.getElementById('path_env');
  if(input.dataset.original!==undefined)input.value=input.dataset.original;
  input.readOnly=true;document.getElementById('path_modify').hidden=false;
  document.getElementById('path_save').hidden=true;document.getElementById('path_cancel').hidden=true;
}
function syncMode(){
  const mode=document.getElementById('mode');const cron=document.getElementById('cron-field');
  if(mode&&cron)cron.style.display=mode.value.startsWith('cron')?'grid':'none';
}
function confirmDelete(form){
  const message=document.body.dataset.deletePrompt.replace('%s',form.dataset.name);
  return confirm(message);
}
document.addEventListener('DOMContentLoaded',syncMode);
</script>
</head>
<body data-delete-prompt="{{.L.T "delete_confirm"}}">
<main class="shell">
  <header class="topbar">
    <div class="brand"><div class="mark">PG</div><div><h1>ProcessGod</h1><p>{{.L.T "native_subtitle"}}</p></div></div>
    <div class="daemon"><span class="dot {{if .Online}}online{{end}}"></span><div class="daemon-copy"><strong>{{if .Online}}{{.L.T "guardian_running"}}{{else}}{{.L.T "guardian_stopped"}}{{end}}</strong><small>{{.ServiceMode}}</small></div><div class="lang-switch"><a class="{{if eq .Lang "en"}}active{{end}}" href="/language?lang=en&next={{urlquery .CurrentURL}}">EN</a><a class="{{if eq .Lang "zh-CN"}}active{{end}}" href="/language?lang=zh-CN&next={{urlquery .CurrentURL}}">简中</a></div></div>
  </header>

  <nav class="toolbar">
    <a class="button primary" href="/?new=1">+ {{.L.T "add_process"}}</a>
    {{if .Online}}
    <form class="inline" method="post" action="/action"><input type="hidden" name="action" value="stop-daemon"><button class="ghost">{{.L.T "stop_guardian"}}</button></form>
    {{else}}
    <form class="inline" method="post" action="/action"><input type="hidden" name="action" value="start-daemon"><button>{{.L.T "start_guardian"}}</button></form>
    {{end}}
    <form class="inline" method="post" action="/action"><input type="hidden" name="action" value="reload"><button class="ghost" {{if .ReloadDisabled}}disabled{{end}}>{{.L.T "reload"}}</button></form>
    <span class="summary">{{.Summary}}</span>
    <span class="spacer"></span>
    <span class="summary">{{.ServiceHint}}</span>
  </nav>

  {{if .FlashErr}}<div class="flash error">{{.FlashErr}}</div>{{end}}
  {{if .StatusErr}}<div class="flash error">{{.L.T "guardian_status"}}: {{.StatusErr}}</div>{{end}}
  {{if .FlashOK}}<div class="flash ok">{{.FlashOK}}</div>{{end}}

  <div class="layout">
    <section class="panel">
      <div class="panel-head"><div><h2>{{.L.T "processes"}}</h2><small>{{.L.T "edit_hint"}}</small></div><small>{{.L.Format "total" .ConfiguredCount}}</small></div>
      <div class="tasks">
        {{range .Rows}}
        <article class="task">
          <div class="task-main"><span class="dot {{.StateClass}}"></span><div><div class="task-name">{{.Item.ProcessName}}</div><div class="task-id">{{.Item.ID}}</div></div></div>
          <div><div class="command">{{.Item.ExecPath}} {{.Item.StartupParams}}</div><span class="mode">{{.ModeLabel}}</span></div>
          <div class="state {{.StateClass}}" title="{{.StateLabel}}">{{.StateLabel}}</div>
          <div class="actions">
            <form class="inline" method="post" action="/action"><input type="hidden" name="action" value="toggle-item"><input type="hidden" name="id" value="{{.Item.ID}}"><button class="ghost">{{.ToggleLabel}}</button></form>
            <form class="inline" method="post" action="/action"><input type="hidden" name="action" value="restart-item"><input type="hidden" name="id" value="{{.Item.ID}}"><button class="ghost" {{if or (not .Item.Started) (not $.Online)}}disabled{{end}}>{{$.L.T "restart"}}</button></form>
            {{if .Has}}<a class="button ghost" href="/logs?id={{.Item.ID}}&lines=120" target="_blank">{{$.L.T "logs"}}</a>{{end}}
            <a class="button ghost" href="/?edit={{.Item.ID}}">{{$.L.T "edit"}}</a>
            <form class="inline" method="post" action="/action" onsubmit="return confirmDelete(this)" data-name="{{.Item.ProcessName}}"><input type="hidden" name="action" value="delete-item"><input type="hidden" name="id" value="{{.Item.ID}}"><button class="danger">{{$.L.T "delete"}}</button></form>
          </div>
        </article>
        {{else}}
        <div class="empty"><strong>{{.L.T "no_processes"}}</strong>{{.L.T "no_processes_body"}}</div>
        {{end}}
      </div>

      <details class="settings">
        <summary>{{.L.T "path_settings"}}</summary>
        <form method="post" action="/action">
          <input type="hidden" name="action" value="save-path">
          <div class="path-row"><input type="text" id="path_env" name="path_env" value="{{.PathEnv}}" readonly><button type="button" id="path_modify" class="ghost" onclick="enablePathEdit()">{{.L.T "modify"}}</button><button type="submit" id="path_save" hidden>{{.L.T "save"}}</button><button type="button" id="path_cancel" class="danger" hidden onclick="cancelPathEdit()">{{.L.T "cancel"}}</button></div>
          <div class="hint">{{.L.T "path_hint"}}</div>
        </form>
      </details>
    </section>

    <aside class="panel inspector">
      {{if .ShowEditor}}
      <div class="inspector-head"><h2>{{if .EditFound}}{{.L.T "edit_process"}}{{else}}{{.L.T "new_process"}}{{end}}</h2><p>{{if .EditFound}}{{.Edit.ID}}{{else}}{{.L.T "configure_process"}}{{end}}</p></div>
      <form method="post" action="/action" class="form-body">
        <input type="hidden" name="action" value="save-item"><input type="hidden" name="original_id" value="{{.Edit.ID}}">
        <div class="field"><label for="process_name">{{.L.T "display_name"}}</label><input id="process_name" type="text" name="process_name" value="{{.Edit.ProcessName}}" placeholder="{{.L.T "display_name_example"}}" required></div>
        <div class="field"><label for="exec_path">{{.L.T "command"}}</label><input id="exec_path" type="text" name="exec_path" value="{{.Edit.ExecPath}}" placeholder="{{.L.T "command_example"}}" required></div>
        <div class="field"><label for="startup_params">{{.L.T "arguments"}}</label><input id="startup_params" type="text" name="startup_params" value="{{.Edit.StartupParams}}" placeholder="{{.L.T "arguments_example"}}"></div>
        <div class="field"><label for="mode">{{.L.T "behavior"}}</label><select id="mode" name="mode" onchange="syncMode()"><option value="guard" {{if eq .EditMode "guard"}}selected{{end}}>{{.L.T "mode_guard_full"}}</option><option value="once" {{if eq .EditMode "once"}}selected{{end}}>{{.L.T "mode_once_full"}}</option><option value="cron-run" {{if eq .EditMode "cron-run"}}selected{{end}}>{{.L.T "mode_cron_run_full"}}</option><option value="cron-restart" {{if eq .EditMode "cron-restart"}}selected{{end}}>{{.L.T "mode_cron_restart_full"}}</option></select></div>
        <div class="field" id="cron-field"><label for="cron_expression">{{.L.T "cron_schedule"}}</label><input id="cron_expression" type="text" name="cron_expression" value="{{.Edit.CronExpression}}" placeholder="0 1 * * *"></div>
        <label class="check"><input type="checkbox" name="started" {{if .Edit.Started}}checked{{end}}> {{.L.T "enabled_immediately"}}</label>
        <details class="advanced"><summary>{{.L.T "advanced_options"}}</summary><div class="inside">
          <div class="field"><label for="id">{{.L.T "stable_id"}}</label><input id="id" type="text" name="id" value="{{.Edit.ID}}" placeholder="{{.L.T "generated_from_name"}}"></div>
          <div class="field"><label for="working_dir">{{.L.T "working_directory"}}</label><input id="working_dir" type="text" name="working_dir" value="{{.Edit.WorkingDir}}" placeholder="{{.L.T "optional"}}"></div>
          <label class="check"><input type="checkbox" name="no_window" {{if .Edit.NoWindow}}checked{{end}}> {{.L.T "no_window"}}</label>
          <label class="check"><input type="checkbox" name="minimize" {{if .Edit.Minimize}}checked{{end}}> {{.L.T "start_minimized"}}</label>
        </div></details>
        <div class="form-actions"><button type="submit">{{if .EditFound}}{{.L.T "save_changes"}}{{else}}{{.L.T "add_process"}}{{end}}</button><a class="button ghost" href="/">{{.L.T "cancel"}}</a></div>
      </form>
      {{else}}
      <div class="inspector-empty"><strong>{{.L.T "select_process"}}</strong>{{.L.T "select_process_body"}}<div style="margin-top:18px"><a class="button primary" href="/?new=1">+ {{.L.T "add_process"}}</a></div></div>
      {{end}}
    </aside>
  </div>
</main>
</body>
</html>`))
