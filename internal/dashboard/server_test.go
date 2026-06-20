package dashboard

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lovitus/processgod-mac/internal/config"
)

func TestTranslationCatalogsHaveMatchingKeys(t *testing.T) {
	for key := range en {
		if _, ok := zhCN[key]; !ok {
			t.Errorf("missing zh-CN translation for %q", key)
		}
	}
	for key := range zhCN {
		if _, ok := en[key]; !ok {
			t.Errorf("missing English translation for %q", key)
		}
	}
}

func TestSaveItemMapsBehaviorMode(t *testing.T) {
	tests := []struct {
		mode        string
		once        bool
		cron        string
		restartCron bool
	}{
		{mode: "guard", restartCron: true},
		{mode: "once", once: true},
		{mode: "cron-run", cron: "*/5 * * * *"},
		{mode: "cron-restart", cron: "*/5 * * * *", restartCron: true},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := config.Save(path, config.Config{}); err != nil {
				t.Fatalf("save initial config: %v", err)
			}
			s := &Server{ConfigPath: path, ControlAddr: "127.0.0.1:1"}
			form := url.Values{
				"process_name":    {"Example"},
				"exec_path":       {"echo"},
				"mode":            {tt.mode},
				"cron_expression": {"*/5 * * * *"},
				"started":         {"on"},
			}
			req := httptest.NewRequest("POST", "/action", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if err := req.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if err := s.saveItem(req); err != nil {
				t.Fatalf("save item: %v", err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			item := cfg.Items[0]
			if item.OnlyOpenOnce != tt.once || item.CronExpression != tt.cron || item.StopBeforeCronExec != tt.restartCron {
				t.Fatalf("unexpected mode mapping: %+v", item)
			}
		})
	}
}

func TestNewProcessDefaultsToAlwaysGuard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	s := &Server{ConfigPath: path, ControlAddr: "127.0.0.1:1"}
	req := httptest.NewRequest("GET", "/?new=1", nil)
	w := httptest.NewRecorder()
	s.handleIndex(w, req)

	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, body)
	}
	if !strings.Contains(body, `<option value="guard" selected>`) {
		t.Fatalf("new process did not default to always guard")
	}
	if !strings.Contains(body, "New Process") || !strings.Contains(body, "Processes") {
		t.Fatalf("manager layout missing expected sections")
	}
}

func TestChineseLanguageFromBrowserAndCookie(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	s := &Server{ConfigPath: path, ControlAddr: "127.0.0.1:1"}

	req := httptest.NewRequest("GET", "/?new=1&ok=process_restarted", nil)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	w := httptest.NewRecorder()
	s.handleIndex(w, req)
	body := w.Body.String()
	for _, expected := range []string{`<html lang="zh-CN">`, "新增进程", "持续守护 - 退出后自动重启", "进程已重启"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("Chinese page missing %q", expected)
		}
	}

	languageReq := httptest.NewRequest("GET", "/language?lang=zh-CN&next=%2F%3Fnew%3D1", nil)
	languageW := httptest.NewRecorder()
	s.handleLanguage(languageW, languageReq)
	if languageW.Code != 303 || languageW.Header().Get("Location") != "/?new=1" {
		t.Fatalf("unexpected language redirect: status=%d location=%q", languageW.Code, languageW.Header().Get("Location"))
	}
	result := languageW.Result()
	defer result.Body.Close()
	foundCookie := false
	for _, cookie := range result.Cookies() {
		if cookie.Name == languageCookie && cookie.Value == "zh-CN" {
			foundCookie = true
		}
	}
	if !foundCookie {
		t.Fatalf("language cookie was not set")
	}
}

func TestEditingPreservesEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	initial := config.Config{Items: []config.Item{{
		ID: "example", ProcessName: "Example", ExecPath: "echo", Started: true,
		Env: map[string]string{"TOKEN": "secret"},
	}}}
	if err := config.Save(path, initial); err != nil {
		t.Fatalf("save config: %v", err)
	}
	s := &Server{ConfigPath: path, ControlAddr: "127.0.0.1:1"}
	form := url.Values{
		"original_id": {"example"}, "id": {"example"}, "process_name": {"Renamed"},
		"exec_path": {"echo"}, "mode": {"guard"}, "started": {"on"},
	}
	req := httptest.NewRequest("POST", "/action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}
	if err := s.saveItem(req); err != nil {
		t.Fatalf("save item: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Items[0].Env["TOKEN"] != "secret" {
		t.Fatalf("environment was lost: %+v", cfg.Items[0].Env)
	}
}
