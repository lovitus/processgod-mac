# ProcessGodMac User Guide

## 1) Service Levels

ProcessGodMac daemon can run in three levels:

- `system`: LaunchDaemon, starts before login screen (requires `sudo`).
- `user`: LaunchAgent, starts after user login.
- `manual`: started manually by tray/CLI process.

To enable pre-login startup:

```bash
sudo /Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac service install --system
```

## 2) Dashboard

Open:

- auto-open from tray, or
- `http://127.0.0.1:51090/`

### Quick Add (recommended)

Fields:

- Name: display name
- Command: command name or absolute path
- Arguments: command args
- Mode:
  - Always Guard: restart on crash
  - Start Once: run once only
  - Cron Restart: run/restart by cron

### Advanced Add

Use all raw fields (`id`, `exec_path`, `cron_expression`, flags).

### PATH Editor

Dashboard has PATH textbox with:

- `modify`
- `save`
- `discard/cancel`

Saved PATH is used by daemon for command lookup.

## 3) Item Controls

- `edit`: load item into Edit form
- `toggle(started|stopped)`: enable/disable guarding for that item
- `delete`
- `logs`: open memory-only logs

## 4) Logs (Memory-only)

Per task buffers:

- `error_warning`: latest 100 lines
- `standard_other`: latest 20 lines

Output includes line sequence markers:

- `E#<n>` for error/warning
- `S#<n>` for standard/other

No task logs are persisted to disk.
