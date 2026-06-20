package dashboard

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const languageCookie = "processgod_language"

type localizer struct {
	lang string
}

func localizerForRequest(r *http.Request) localizer {
	if lang := normalizeLanguage(r.URL.Query().Get("lang")); lang != "" {
		return localizer{lang: lang}
	}
	if cookie, err := r.Cookie(languageCookie); err == nil {
		if lang := normalizeLanguage(cookie.Value); lang != "" {
			return localizer{lang: lang}
		}
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Accept-Language"))), "zh") {
		return localizer{lang: "zh-CN"}
	}
	return localizer{lang: "en"}
}

func normalizeLanguage(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"))
	switch value {
	case "en", "en-us", "en-gb":
		return "en"
	case "zh", "zh-cn", "zh-hans", "zh-sg":
		return "zh-CN"
	default:
		return ""
	}
}

func (l localizer) Lang() string {
	return l.lang
}

func (l localizer) T(key string) string {
	if l.lang == "zh-CN" {
		if value, ok := zhCN[key]; ok {
			return value
		}
	}
	if value, ok := en[key]; ok {
		return value
	}
	return key
}

func (l localizer) Format(key string, args ...any) string {
	return fmt.Sprintf(l.T(key), args...)
}

func setLanguageCookie(w http.ResponseWriter, lang string) {
	http.SetCookie(w, &http.Cookie{
		Name:     languageCookie,
		Value:    lang,
		Path:     "/",
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
		SameSite: http.SameSiteLaxMode,
	})
}

var en = map[string]string{
	"native_subtitle":        "Native process guardian for macOS",
	"guardian_running":       "Guardian running",
	"guardian_stopped":       "Guardian stopped",
	"add_process":            "Add Process",
	"stop_guardian":          "Stop Guardian",
	"start_guardian":         "Start Guardian",
	"reload":                 "Reload",
	"status_summary":         "%d running / %d enabled / %d configured",
	"guardian_status":        "Guardian status",
	"processes":              "Processes",
	"edit_hint":              "Click Edit for full configuration",
	"total":                  "%d total",
	"start":                  "Start",
	"stop":                   "Stop",
	"restart":                "Restart",
	"logs":                   "Logs",
	"edit":                   "Edit",
	"delete":                 "Delete",
	"delete_confirm":         "Delete %s and stop it?",
	"no_processes":           "No processes configured",
	"no_processes_body":      "Add the first command you want ProcessGod to keep alive.",
	"path_settings":          "Command PATH and advanced settings",
	"modify":                 "Modify",
	"save":                   "Save",
	"cancel":                 "Cancel",
	"path_hint":              "Used to resolve command names such as node, java and python. Absolute paths work without this setting.",
	"edit_process":           "Edit Process",
	"new_process":            "New Process",
	"configure_process":      "Configure one command and its restart behavior",
	"display_name":           "Display Name",
	"display_name_example":   "API Server",
	"command":                "Command",
	"command_example":        "node or /usr/local/bin/my-server",
	"arguments":              "Arguments",
	"arguments_example":      "server.js --port 8080",
	"behavior":               "Behavior",
	"mode_guard_full":        "Always Guard - restart after exit",
	"mode_once_full":         "Start Once - do not restart",
	"mode_cron_run_full":     "Cron Run - start only on schedule",
	"mode_cron_restart_full": "Cron Restart - guard and restart on schedule",
	"cron_schedule":          "Cron Schedule",
	"enabled_immediately":    "Enabled immediately",
	"advanced_options":       "Advanced options",
	"stable_id":              "Stable ID",
	"generated_from_name":    "generated from name",
	"working_directory":      "Working Directory",
	"optional":               "optional",
	"no_window":              "No window",
	"start_minimized":        "Start minimized",
	"save_changes":           "Save Changes",
	"select_process":         "Select a process to edit",
	"select_process_body":    "Use the row actions for daily control, or add a new process.",
	"mode_guard":             "Always Guard",
	"mode_once":              "Start Once",
	"mode_cron_run":          "Cron Run",
	"mode_cron_restart":      "Cron Restart",
	"state_disabled":         "Disabled",
	"state_offline":          "Guardian offline",
	"state_running":          "Running - PID %d",
	"state_error":            "Error: %s",
	"state_completed":        "Completed",
	"state_wait_schedule":    "Waiting for schedule",
	"state_wait_start":       "Waiting to start",
	"service_system":         "System (before login)",
	"service_user":           "User (after login)",
	"service_manual":         "Manual",
	"service_unknown":        "Unknown",
	"hint_system":            "Starts before login as a system LaunchDaemon.",
	"hint_user":              "Starts after login as a user LaunchAgent.",
	"hint_manual":            "Started manually. Choose a startup mode from the menu bar.",
	"hint_offline":           "Guardian is not reachable. Start it from this page or the menu bar.",
	"edit_target_not_found":  "Edit target %q was not found",
	"daemon_started":         "Guardian started",
	"daemon_stopped":         "Guardian stopped",
	"config_reloaded":        "Configuration reloaded",
	"item_toggled":           "Process state updated",
	"process_restarted":      "Process restarted",
	"item_deleted":           "Process deleted",
	"item_saved":             "Process saved",
	"item_added":             "Process added",
	"path_updated":           "PATH updated",
}

var zhCN = map[string]string{
	"native_subtitle":        "macOS 原生进程守护工具",
	"guardian_running":       "守护进程运行中",
	"guardian_stopped":       "守护进程已停止",
	"add_process":            "新增进程",
	"stop_guardian":          "停止守护进程",
	"start_guardian":         "启动守护进程",
	"reload":                 "重新加载",
	"status_summary":         "%d 个运行中 / %d 个已启用 / 共 %d 个",
	"guardian_status":        "守护进程状态",
	"processes":              "进程列表",
	"edit_hint":              "点击编辑可修改完整配置",
	"total":                  "共 %d 个",
	"start":                  "启动",
	"stop":                   "停止",
	"restart":                "立即重启",
	"logs":                   "日志",
	"edit":                   "编辑",
	"delete":                 "删除",
	"delete_confirm":         "删除 %s 并停止该进程？",
	"no_processes":           "尚未配置进程",
	"no_processes_body":      "新增第一个需要 ProcessGod 持续守护的命令。",
	"path_settings":          "命令 PATH 与高级设置",
	"modify":                 "修改",
	"save":                   "保存",
	"cancel":                 "取消",
	"path_hint":              "用于查找 node、java、python 等命令。使用绝对路径时不依赖此设置。",
	"edit_process":           "编辑进程",
	"new_process":            "新增进程",
	"configure_process":      "配置命令及其重启行为",
	"display_name":           "显示名称",
	"display_name_example":   "API 服务",
	"command":                "命令",
	"command_example":        "node 或 /usr/local/bin/my-server",
	"arguments":              "启动参数",
	"arguments_example":      "server.js --port 8080",
	"behavior":               "运行模式",
	"mode_guard_full":        "持续守护 - 退出后自动重启",
	"mode_once_full":         "仅运行一次 - 退出后不重启",
	"mode_cron_run_full":     "Cron 启动 - 仅按计划启动",
	"mode_cron_restart_full": "Cron 重启 - 持续守护并按计划强制重启",
	"cron_schedule":          "Cron 计划",
	"enabled_immediately":    "保存后立即启用",
	"advanced_options":       "高级选项",
	"stable_id":              "固定 ID",
	"generated_from_name":    "根据名称自动生成",
	"working_directory":      "工作目录",
	"optional":               "可选",
	"no_window":              "不显示窗口",
	"start_minimized":        "最小化启动",
	"save_changes":           "保存修改",
	"select_process":         "选择一个进程进行编辑",
	"select_process_body":    "日常操作可直接在列表中完成，也可以新增进程。",
	"mode_guard":             "持续守护",
	"mode_once":              "仅运行一次",
	"mode_cron_run":          "Cron 启动",
	"mode_cron_restart":      "Cron 重启",
	"state_disabled":         "已停用",
	"state_offline":          "守护进程离线",
	"state_running":          "运行中 - PID %d",
	"state_error":            "错误：%s",
	"state_completed":        "已完成",
	"state_wait_schedule":    "等待计划触发",
	"state_wait_start":       "等待启动",
	"service_system":         "系统级（登录前）",
	"service_user":           "用户级（登录后）",
	"service_manual":         "手动运行",
	"service_unknown":        "未知",
	"hint_system":            "以系统 LaunchDaemon 运行，在用户登录前启动。",
	"hint_user":              "以用户 LaunchAgent 运行，在用户登录后启动。",
	"hint_manual":            "当前为手动启动，可在菜单栏选择自动启动级别。",
	"hint_offline":           "无法连接守护进程，请从本页或菜单栏启动。",
	"edit_target_not_found":  "找不到要编辑的项目 %q",
	"daemon_started":         "守护进程已启动",
	"daemon_stopped":         "守护进程已停止",
	"config_reloaded":        "配置已重新加载",
	"item_toggled":           "进程状态已更新",
	"process_restarted":      "进程已重启",
	"item_deleted":           "进程已删除",
	"item_saved":             "进程已保存",
	"item_added":             "进程已新增",
	"path_updated":           "PATH 已更新",
}
