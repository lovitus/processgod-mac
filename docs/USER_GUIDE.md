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

## 2) Menu Bar

Opening `ProcessGodMac.app` creates the `PG` menu bar item. It does not open a browser automatically.

The menu shows every configured process. Each process submenu provides:

- current state and PID
- enable guard / disable guard and stop
- restart now
- view memory logs
- edit
- delete with native confirmation

The `Startup` submenu installs user mode (after login), system mode (before login), or removes automatic startup.

## 3) Process Manager

Open:

- `Manage Processes...` from the tray, or
- `http://127.0.0.1:51090/`

The manager uses a process list and one add/edit inspector. Daily controls stay on each process row.

### Add / Edit

Fields:

- Name: display name
- Command: command name or absolute path
- Arguments: command args
- Always Guard: restart after exit
- Start Once: run once only
- Cron Run: start only when cron matches
- Cron Restart: continuously guard and force restart when cron matches

Stable ID, working directory, no-window, and minimize settings are under `Advanced options`.

### PATH Editor

Dashboard has PATH textbox with:

- `modify`
- `save`
- `discard/cancel`

Saved PATH is used by daemon for command lookup.

## 4) Item Controls

- `Start` / `Stop`: enable or disable guarding
- `Restart`: stop and immediately start an enabled process
- `Edit`: load the item into the inspector
- `delete`
- `logs`: open memory-only logs

## 5) Logs (Memory-only)

Per task buffers:

- `error_warning`: latest 100 lines
- `standard_other`: latest 20 lines
- each stored log line is truncated to max 4096 bytes

Output includes line sequence markers:

- `E#<n>` for error/warning
- `S#<n>` for standard/other

No task logs are persisted to disk. The in-memory cache does not grow by line count beyond these caps.
