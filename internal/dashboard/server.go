package dashboard

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

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

func (s *Server) Run(openBrowser bool) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/action", s.handleAction)
	mux.HandleFunc("/logs", s.handleLogs)

	if openBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
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
	edit := config.Item{
		ID:                 "",
		ProcessName:        "",
		ExecPath:           "",
		StartupParams:      "",
		WorkingDir:         "",
		Started:            true,
		StopBeforeCronExec: true,
		CronExpression:     "0 1 * * *",
	}
	if editID != "" {
		for _, it := range cfg.Items {
			if it.ID == editID {
				edit = it
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
	type row struct {
		Item   config.Item
		Status guardian.Status
		Has    bool
	}
	rows := make([]row, 0, len(cfg.Items))
	for _, it := range cfg.Items {
		st, ok := statusByID[it.ID]
		rows = append(rows, row{Item: it, Status: st, Has: ok})
	}

	data := struct {
		Rows      []row
		Edit      config.Item
		Online    bool
		StatusErr string
		FlashErr  string
		Addr      string
		CfgPath   string
	}{
		Rows:      rows,
		Edit:      edit,
		Online:    online,
		StatusErr: statusErr,
		FlashErr:  strings.TrimSpace(r.URL.Query().Get("error")),
		Addr:      s.ControlAddr,
		CfgPath:   s.ConfigPath,
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
		v, err := strconv.Atoi(n)
		if err == nil && v > 0 {
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

	switch action {
	case "start-daemon":
		err = daemonctl.EnsureRunning(s.ControlAddr, s.ExePath, s.WorkDir)
	case "stop-daemon":
		err = daemonctl.Stop(s.ControlAddr)
	case "reload":
		resp, reqErr := ipc.Send(s.ControlAddr, ipc.Request{Action: "reload"})
		if reqErr != nil {
			err = reqErr
		} else if !resp.OK {
			err = errors.New(resp.Error)
		}
	case "toggle-item":
		err = s.toggleItem(strings.TrimSpace(r.FormValue("id")))
	case "delete-item":
		err = s.deleteItem(strings.TrimSpace(r.FormValue("id")))
	case "save-item":
		err = s.saveItem(r)
	default:
		err = fmt.Errorf("unknown action: %s", action)
	}

	if err != nil {
		http.Redirect(w, r, "/?error="+urlEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
input[type=text]{padding:6px;border:1px solid #c4cdd9;border-radius:6px;min-width:190px}
button{padding:6px 10px;border:1px solid #8ba3c9;background:#1f6feb;color:#fff;border-radius:6px;cursor:pointer}
button.alt{background:#4b5563}
button.warn{background:#b42318}
table{border-collapse:collapse;width:100%}
th,td{border-bottom:1px solid #edf1f6;padding:8px;text-align:left;font-size:13px}
.tag{display:inline-block;padding:2px 7px;border-radius:999px;background:#eef2ff;font-size:12px}
.off{background:#fee2e2}
a{color:#1f6feb;text-decoration:none}
small{color:#475467}
label{margin-right:8px}
</style>
</head>
<body>
<h1>ProcessGodMac Dashboard</h1>
<div class="card">
<div><strong>Daemon:</strong> {{if .Online}}<span class="tag">online</span>{{else}}<span class="tag off">offline</span>{{end}}</div>
<div><small>control: {{.Addr}}</small></div>
<div><small>config: {{.CfgPath}}</small></div>
{{if .StatusErr}}<div><small style="color:#b42318">{{.StatusErr}}</small></div>{{end}}
{{if .FlashErr}}<div><small style="color:#b42318">{{.FlashErr}}</small></div>{{end}}
<div class="row" style="margin-top:8px">
<form method="post" action="/action"><input type="hidden" name="action" value="start-daemon"><button>Start Daemon</button></form>
<form method="post" action="/action"><input type="hidden" name="action" value="stop-daemon"><button class="warn">Stop Daemon</button></form>
<form method="post" action="/action"><input type="hidden" name="action" value="reload"><button class="alt">Reload Config</button></form>
</div>
</div>

<div class="card">
<h3 style="margin-top:0">Save Item</h3>
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
<a href="/" style="margin-left:6px">clear</a>
</div>
</form>
</div>

<div class="card">
<h3 style="margin-top:0">Configured Items</h3>
<table>
<thead><tr><th>id</th><th>name</th><th>exec</th><th>started</th><th>running</th><th>pid</th><th>cron</th><th>actions</th></tr></thead>
<tbody>
{{range .Rows}}
<tr>
<td>{{.Item.ID}}</td>
<td>{{.Item.ProcessName}}</td>
<td><small>{{.Item.ExecPath}}</small></td>
<td>{{.Item.Started}}</td>
<td>{{if .Has}}{{.Status.Running}}{{else}}-{{end}}</td>
<td>{{if .Has}}{{.Status.PID}}{{else}}-{{end}}</td>
<td><small>{{.Item.CronExpression}}</small></td>
<td>
<a href="/?edit={{.Item.ID}}">edit</a>
 |
<form method="post" action="/action" style="display:inline"><input type="hidden" name="action" value="toggle-item"><input type="hidden" name="id" value="{{.Item.ID}}"><button class="alt" style="padding:2px 6px">toggle</button></form>
 |
<form method="post" action="/action" style="display:inline" onsubmit="return confirm('delete {{.Item.ID}}?')"><input type="hidden" name="action" value="delete-item"><input type="hidden" name="id" value="{{.Item.ID}}"><button class="warn" style="padding:2px 6px">delete</button></form>
 |
<a href="/logs?id={{.Item.ID}}&lines=400" target="_blank">logs</a>
</td>
</tr>
{{else}}
<tr><td colspan="8">No items in config</td></tr>
{{end}}
</tbody>
</table>
</div>
</body></html>`))
