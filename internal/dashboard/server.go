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
}

func (s *Server) Run(openBrowser bool) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/action", s.handleAction)
	mux.HandleFunc("/logs", s.handleLogs)

	if openBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			_ = exec.Command("open", "http://"+s.Addr).Run()
		}()
	}

	return http.ListenAndServe(s.Addr, mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		http.Error(w, "load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	editID := strings.TrimSpace(r.URL.Query().Get("edit"))
	edit := config.Item{Started: true, StopBeforeCronExec: true, CronExpression: "0 1 * * *"}
	editFound := false
	if editID != "" {
		for _, it := range cfg.Items {
			if it.ID == editID {
				edit = it
				editFound = true
				break
			}
		}
	}

	statuses, online, statusErr := queryStatus(s.ControlAddr)
	statusByID := make(map[string]guardian.Status, len(statuses))
	for _, st := range statuses {
		statusByID[st.ID] = st
	}

	sort.Slice(cfg.Items, func(i, j int) bool { return cfg.Items[i].ID < cfg.Items[j].ID })
	rows := make([]row, 0, len(cfg.Items))
	for _, it := range cfg.Items {
		st, ok := statusByID[it.ID]
		toggleLabel := "toggle(stopped)"
		if it.Started {
			toggleLabel = "toggle(started)"
		}
		rows = append(rows, row{
			Item:        it,
			Status:      st,
			Has:         ok,
			ModeLabel:   modeLabel(it),
			ToggleLabel: toggleLabel,
		})
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
			flashErr = fmt.Sprintf("edit target %q not found", editID)
		}
	}

	data := struct {
		Rows           []row
		Edit           config.Item
		EditFound      bool
		Online         bool
		StatusErr      string
		FlashErr       string
		FlashOK        string
		Addr           string
		CfgPath        string
		StartLabel     string
		StopLabel      string
		StartDisabled  bool
		StopDisabled   bool
		ReloadDisabled bool
		PathEnv        string
	}{
		Rows:           rows,
		Edit:           edit,
		EditFound:      editFound,
		Online:         online,
		StatusErr:      statusErr,
		FlashErr:       flashErr,
		FlashOK:        strings.TrimSpace(r.URL.Query().Get("ok")),
		Addr:           s.ControlAddr,
		CfgPath:        s.ConfigPath,
		StartLabel:     startLabel,
		StopLabel:      stopLabel,
		StartDisabled:  startDisabled,
		StopDisabled:   stopDisabled,
		ReloadDisabled: reloadDisabled,
		PathEnv:        cfg.PathEnv,
	}

	if err := pageTmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
	okMsg := "saved"

	switch action {
	case "start-daemon":
		err = daemonctl.EnsureRunning(s.ControlAddr, s.ExePath, s.WorkDir)
		okMsg = "daemon started"
	case "stop-daemon":
		err = daemonctl.Stop(s.ControlAddr)
		okMsg = "daemon stopped"
	case "reload":
		err = reloadIfRunning(s.ControlAddr)
		okMsg = "config reloaded"
	case "toggle-item":
		err = s.toggleItem(strings.TrimSpace(r.FormValue("id")))
		okMsg = "item toggled"
	case "delete-item":
		err = s.deleteItem(strings.TrimSpace(r.FormValue("id")))
		okMsg = "item deleted"
	case "save-item":
		err = s.saveItem(r)
		okMsg = "item saved"
	case "quick-add":
		err = s.quickAdd(r)
		okMsg = "item added"
	case "save-path":
		err = s.savePath(strings.TrimSpace(r.FormValue("path_env")))
		okMsg = "PATH updated"
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

func queryStatus(controlAddr string) ([]guardian.Status, bool, string) {
	resp, err := ipc.Send(controlAddr, ipc.Request{Action: "status"})
	if err != nil {
		return nil, false, err.Error()
	}
	if !resp.OK {
		return nil, false, resp.Error
	}
	return resp.Status, true, ""
}

func modeLabel(it config.Item) string {
	if it.OnlyOpenOnce {
		return "Start Once"
	}
	if strings.TrimSpace(it.CronExpression) != "" {
		if it.StopBeforeCronExec {
			return "Cron Restart"
		}
		return "Cron Run"
	}
	return "Always Guard"
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

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8"/>
<title>ProcessGodMac Dashboard</title>
<style>
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:20px;background:#f5f7fb;color:#1f2937}
h1{margin:0 0 12px 0}
.card{background:#fff;border:1px solid #dbe1ea;border-radius:10px;padding:14px;margin-bottom:14px}
.row{display:flex;gap:8px;align-items:center;flex-wrap:wrap}
input[type=text],select{padding:7px;border:1px solid #c4cdd9;border-radius:6px;min-width:180px}
button{padding:7px 11px;border:1px solid #8ba3c9;background:#1f6feb;color:#fff;border-radius:6px;cursor:pointer}
button:disabled{background:#9aa4b2;border-color:#9aa4b2;cursor:not-allowed;opacity:.7}
button.alt{background:#4b5563}
button.warn{background:#b42318}
table{border-collapse:collapse;width:100%}
th,td{border-bottom:1px solid #edf1f6;padding:8px;text-align:left;font-size:13px;vertical-align:top}
.tag{display:inline-block;padding:2px 7px;border-radius:999px;background:#eef2ff;font-size:12px}
.off{background:#fee2e2}
.ok{background:#dcfce7}
a{color:#1f6feb;text-decoration:none}
small{color:#475467}
code{background:#eef2ff;padding:2px 4px;border-radius:4px}
label{margin-right:8px}
.err{color:#b42318}
</style>
<script>
function enablePathEdit(){
  const input=document.getElementById('path_env');
  const modify=document.getElementById('path_modify');
  const save=document.getElementById('path_save');
  const cancel=document.getElementById('path_cancel');
  input.dataset.original=input.value;
  input.readOnly=false;
  modify.style.display='none';
  save.style.display='inline-block';
  cancel.style.display='inline-block';
  input.focus();
}
function cancelPathEdit(){
  const input=document.getElementById('path_env');
  const modify=document.getElementById('path_modify');
  const save=document.getElementById('path_save');
  const cancel=document.getElementById('path_cancel');
  if(input.dataset.original!==undefined){input.value=input.dataset.original;}
  input.readOnly=true;
  modify.style.display='inline-block';
  save.style.display='none';
  cancel.style.display='none';
}
</script>
</head>
<body>
<h1>ProcessGodMac Dashboard</h1>
<div class="card">
<div><strong>Daemon:</strong> {{if .Online}}<span class="tag">online</span>{{else}}<span class="tag off">offline</span>{{end}}</div>
<div><small>control: {{.Addr}}</small></div>
<div><small>config: {{.CfgPath}}</small></div>
{{if .StatusErr}}<div class="err"><small>{{.StatusErr}}</small></div>{{end}}
{{if .FlashErr}}<div class="err"><small>{{.FlashErr}}</small></div>{{end}}
{{if .FlashOK}}<div><small><span class="tag ok">{{.FlashOK}}</span></small></div>{{end}}
<div class="row" style="margin-top:8px">
<form method="post" action="/action"><input type="hidden" name="action" value="start-daemon"><button {{if .StartDisabled}}disabled{{end}}>{{.StartLabel}}</button></form>
<form method="post" action="/action"><input type="hidden" name="action" value="stop-daemon"><button class="warn" {{if .StopDisabled}}disabled{{end}}>{{.StopLabel}}</button></form>
<form method="post" action="/action"><input type="hidden" name="action" value="reload"><button class="alt" {{if .ReloadDisabled}}disabled{{end}}>Reload Config</button></form>
</div>
</div>

<div class="card">
<h3 style="margin-top:0">PATH</h3>
<form method="post" action="/action" class="row">
<input type="hidden" name="action" value="save-path">
<input type="text" id="path_env" name="path_env" value="{{.PathEnv}}" readonly style="min-width:900px;flex:1">
<button type="button" id="path_modify" class="alt" onclick="enablePathEdit()">modify</button>
<button type="submit" id="path_save" style="display:none">save</button>
<button type="button" id="path_cancel" class="warn" style="display:none" onclick="cancelPathEdit()">discard/cancel</button>
</form>
<small>This PATH is used to resolve command names like <code>ping</code>, <code>node</code>, <code>java</code>.</small>
</div>

<div class="card">
<h3 style="margin-top:0">Quick Add (Recommended)</h3>
<div><small>Examples: command <code>ping</code> args <code>amazon.com</code>, or command <code>/usr/bin/python3</code>.</small></div>
<form method="post" action="/action">
<input type="hidden" name="action" value="quick-add">
<div class="row" style="margin-top:8px">
<input type="text" name="quick_name" placeholder="Name (e.g. Curl Test)">
<input type="text" name="quick_command" placeholder="Command (e.g. curl)">
<input type="text" name="quick_args" placeholder="Arguments (e.g. -L https://example.com)">
<select name="quick_mode">
<option value="guard">Always Guard (restart on crash)</option>
<option value="once">Start Once</option>
<option value="cron">Cron Restart</option>
</select>
<input type="text" name="quick_cron" placeholder="cron (for Cron Restart)" value="0 1 * * *">
<button>Add Item</button>
</div>
</form>
</div>

<div class="card">
<h3 style="margin-top:0">Edit Item {{if .EditFound}}: {{.Edit.ID}}{{end}}</h3>
<form method="post" action="/action">
<input type="hidden" name="action" value="save-item">
<input type="hidden" name="original_id" value="{{.Edit.ID}}">
<div class="row">
<input type="text" name="id" placeholder="id" value="{{.Edit.ID}}">
<input type="text" name="process_name" placeholder="process name" value="{{.Edit.ProcessName}}">
<input type="text" name="exec_path" placeholder="exec path" value="{{.Edit.ExecPath}}">
<input type="text" name="startup_params" placeholder="startup params" value="{{.Edit.StartupParams}}">
<input type="text" name="working_dir" placeholder="working dir" value="{{.Edit.WorkingDir}}">
<input type="text" name="cron_expression" placeholder="cron expression" value="{{.Edit.CronExpression}}">
</div>
<div class="row" style="margin-top:8px">
<label><input type="checkbox" name="started" {{if .Edit.Started}}checked{{end}}> started</label>
<label><input type="checkbox" name="only_open_once" {{if .Edit.OnlyOpenOnce}}checked{{end}}> start once</label>
<label><input type="checkbox" name="minimize" {{if .Edit.Minimize}}checked{{end}}> minimize</label>
<label><input type="checkbox" name="no_window" {{if .Edit.NoWindow}}checked{{end}}> no window</label>
<label><input type="checkbox" name="stop_before_cron_exec" {{if .Edit.StopBeforeCronExec}}checked{{end}}> restart on cron</label>
<button>Save Item</button>
<a href="/" style="margin-left:6px">clear edit</a>
</div>
</form>
</div>

<div class="card">
<h3 style="margin-top:0">Configured Items</h3>
<table>
<thead><tr><th>id</th><th>name</th><th>command</th><th>mode</th><th>started</th><th>running</th><th>pid</th><th>last error</th><th>actions</th></tr></thead>
<tbody>
{{range .Rows}}
<tr>
<td>{{.Item.ID}}</td>
<td>{{.Item.ProcessName}}</td>
<td><small>{{.Item.ExecPath}} {{.Item.StartupParams}}</small></td>
<td><small>{{.ModeLabel}}</small></td>
<td>{{.Item.Started}}</td>
<td>{{if .Has}}{{.Status.Running}}{{else}}-{{end}}</td>
<td>{{if .Has}}{{.Status.PID}}{{else}}-{{end}}</td>
<td><small class="err">{{if .Has}}{{.Status.LastError}}{{end}}</small></td>
<td>
<a href="/?edit={{.Item.ID}}">edit</a> |
<form method="post" action="/action" style="display:inline"><input type="hidden" name="action" value="toggle-item"><input type="hidden" name="id" value="{{.Item.ID}}"><button class="alt" style="padding:2px 6px">{{.ToggleLabel}}</button></form> |
<form method="post" action="/action" style="display:inline" onsubmit="return confirm('delete {{.Item.ID}}?')"><input type="hidden" name="action" value="delete-item"><input type="hidden" name="id" value="{{.Item.ID}}"><button class="warn" style="padding:2px 6px">delete</button></form> |
<a href="/logs?id={{.Item.ID}}&lines=400" target="_blank">logs</a>
</td>
</tr>
{{else}}
<tr><td colspan="9">No items configured. Use Quick Add above.</td></tr>
{{end}}
</tbody>
</table>
</div>
</body></html>`))
