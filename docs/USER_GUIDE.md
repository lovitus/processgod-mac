# ProcessGodMac User Guide

## Menu Bar

Click the ProcessGod icon to open the 420 x 560 native popover. It provides:

- guardian health and current User/System level
- running and enabled task counts
- persistent Pause All / Resume All
- per-task enable/disable, restart, memory log preview, edit, and full logs
- shortcuts to add, manage, settings, and quit the UI

Quitting the UI leaves the Go daemon and guarded tasks running.

## Add Or Edit A Task

Open **Manage** or **Add Process**. The manager has one process list and one focused editor.

- **Display name**: human-readable task name
- **Executable**: command name such as `node`, or an absolute path; the picker can select a file
- **Arguments**: shell-style text supporting quotes and escapes
- **Working directory**: optional; the picker can select a directory
- **Environment Variables**: task-specific key/value overrides
- **Stable ID**: available under Advanced and immutable after creation
- **Enabled immediately**: starts supervision after save

Modes:

- **Always Guard**: start immediately and restart after an exit
- **Start Once**: start once per daemon/config activation and do not restart after exit
- **Cron Run**: start when the five-field Cron expression matches; wait for the next trigger after exit
- **Cron Restart**: continuously guard and force one restart for each matching minute

Saving uses an expected configuration revision. If another client changed the config, the app reloads the latest revision and asks the user to reapply the edit rather than overwriting it.

## Command PATH

Open **Settings > Command PATH**. Select **Modify**, edit the value, then choose **Save** or **Cancel**. The daemon applies PATH to command lookup and child processes. Task-specific environment variables are applied after the global PATH.

## Startup Level

- **After login (User)**: LaunchAgent for the current account; installed automatically on first launch
- **Before login (System)**: LaunchDaemon available before the login window; administrator account and macOS approval required

The settings and popover show the active level. During a switch, ProcessGod exports the complete storage configuration, verifies the target daemon, imports it, and only then unregisters the old service.

## Logs

The preview and full log window show the same daemon memory:

- Error / Warning: 100 retained lines
- Standard / Other: 20 retained lines
- 4096 bytes maximum per stored line

The header displays `kept/capacity`. Sequence values count all observed lines, including lines that have rotated out. No task output is written to disk.

## Language

Choose **Follow System**, **English**, or **Simplified Chinese** in Settings. The override is stored in user defaults and only affects the native UI.
