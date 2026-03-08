# ProcessGod macOS

macOS-native rewrite of `lovitus/processgod`.

This version is implemented in Go and focuses on service-style process guarding on macOS:

- launchd service mode (user LaunchAgent and system LaunchDaemon)
- boot startup support in `--system` mode (before user login)
- process auto-restart guard
- cron-triggered restart/run behavior
- in-memory log ring cache per guarded process
- CLI status/log inspection and live config reload

## Build

```bash
mkdir -p /tmp/gocache /tmp/gomodcache
GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go build -o dist/processgod-mac ./cmd/processgod
```

## Run Daemon

```bash
./dist/processgod-mac daemon
```

If you open `ProcessGodMac.app` from Finder:

- a menu bar tray icon (`PG`) is created
- guardian is auto-started
- tray menu can `Start/Stop/Reload/Show Status/Open Dashboard/Open Config/Quit`
- dashboard auto-opens for full config management (add/edit/delete/toggle items + view logs)
- tray shows daemon service level (`user` / `system` / `manual`) and hint for switching to system mode

## Service Mode (launchd)

User mode (starts at user login):

```bash
./dist/processgod-mac service install
./dist/processgod-mac service status
```

System mode (starts on boot, requires sudo):

```bash
sudo ./dist/processgod-mac service install --system
sudo ./dist/processgod-mac service status --system
```

System mode is the one that starts before login screen.

Other operations:

```bash
./dist/processgod-mac service start [--system]
./dist/processgod-mac service stop [--system]
./dist/processgod-mac service uninstall [--system]
```

## Config

Config file path:

```bash
./dist/processgod-mac config path
```

Write a sample:

```bash
./dist/processgod-mac config sample
```

Validate config:

```bash
./dist/processgod-mac config validate
```

Default path is `~/Library/Application Support/ProcessGodMac/config.json`.

For sandboxed/dev environments, you can override with:

```bash
export PROCESSGOD_HOME=/path/to/runtime-dir
```

## Runtime Commands

```bash
./dist/processgod-mac reload
./dist/processgod-mac status
./dist/processgod-mac logs <id> --lines 200
./dist/processgod-mac dashboard
```

Additional docs:

- [User Guide](docs/USER_GUIDE.md)
- [Operations](docs/OPERATIONS.md)

Dashboard provides original-app equivalent config workflow:

- add/edit/delete guarded process items
- toggle per-item guard state
- start/stop daemon
- reload config
- view per-item in-memory logs
- quick-add guided form for non-technical users
- command-name support via PATH lookup (e.g. `ping`, `node`, `java`)
- advanced add form for full-field input
- daemon level + system-mode hint shown in dashboard header
- PATH editor with `modify` / `save` / `discard/cancel`

Log retention policy (memory-only, no disk log files):

- each task has 2 rotating buffers
- `error_warning` keeps latest `100` lines
- `standard_other` keeps latest `20` lines
- each stored line is capped to `4096` bytes (`line_max_bytes`)
- logs include line sequence numbers (`E#<n>` / `S#<n>`) and buffer size summary
- old lines rotate out in memory only
- effective per-task log cache is bounded to about `120 * 4096` bytes of text (+ Go object overhead), not unbounded growth

## Cron Semantics

Equivalent behavior to original ProcessGuard:

- `onlyOpenOnce=true`: process starts once and is not restarted after exit.
- `cronExpression` set and `stopBeforeCronExec=false`: cron runs task when trigger matches; if process exits, it stays stopped until next cron trigger.
- `cronExpression` set and `stopBeforeCronExec=true`: process is guarded continuously and cron trigger forces restart (kill+start) once per matching minute.

## Packaging DMG

```bash
./scripts/package-dmg.sh 0.1.0 dev
```

Output naming format:

- `processgod-mac-<version>-<channel>.dmg`
- example: `processgod-mac-0.1.0-dev.dmg`

## Config Schema

```json
{
  "items": [
    {
      "id": "sample-echo",
      "processName": "Sample Echo",
      "execPath": "/bin/sh",
      "args": ["-lc", "while true; do date; sleep 5; done"],
      "workingDir": "/tmp",
      "env": { "JAVA_HOME": "/opt/homebrew/opt/openjdk" },
      "started": true,
      "onlyOpenOnce": false,
      "noWindow": true,
      "cronExpression": "0 1 * * *",
      "stopBeforeCronExec": true
    }
  ]
}
```
