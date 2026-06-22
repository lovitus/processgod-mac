# ProcessGodMac Operations

## Paths

User mode:

- config: `~/Library/Application Support/ProcessGodMac/config.json`
- socket: `~/Library/Application Support/ProcessGodMac/run/user.sock` (`0600`)

System mode:

- config/state: `/Library/Application Support/ProcessGodMac/`
- socket: `/var/run/processgod-mac/system.sock` (`0660`, root:admin)

There are no HTTP or TCP control listeners.

## CLI

Set a convenience variable:

```bash
PG=/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac
```

Commands:

```bash
$PG version
$PG status [--system]
$PG logs [--system] [--lines N] <id>
$PG restart [--system] <id>
$PG pause [--system]
$PG resume [--system]
$PG reload [--system]
$PG config path [--system]
$PG config validate [--system]
```

`--lines` limits the returned view of the already-bounded buffers. It never increases daemon retention.

The `service` subcommands remain for compatibility with pre-native installations. Normal installations and mode switches use `SMAppService` from the native app.

## Health Checks

```bash
$PG status
lsof -U | grep processgod-mac
launchctl print gui/$(id -u)/com.lovitus.processgod.mac.guardian.user
sudo launchctl print system/com.lovitus.processgod.mac.guardian.system
```

`status` reports task state, PID, last start, and current error. A damaged config does not prevent IPC startup: the app connects in degraded mode so the user can repair or import configuration.

## Troubleshooting

### No menu bar icon

Confirm macOS 15+, arm64, and that the app executable launches:

```bash
file /Applications/ProcessGodMac.app/Contents/MacOS/ProcessGodMac
open /Applications/ProcessGodMac.app
```

For a release artifact, verify:

```bash
codesign --verify --deep --strict /Applications/ProcessGodMac.app
spctl --assess --type execute --verbose=2 /Applications/ProcessGodMac.app
```

### Command not found

Modify Command PATH in native Settings, or select an absolute executable. Verify the task working directory and environment table.

### System service awaiting approval

Open **System Settings > General > Login Items & Extensions**, approve ProcessGodMac, then select System mode again. User mode remains active while approval is pending.

### Repair config

The daemon is the only config writer. Prefer the native editor. For manual recovery, stop the relevant service, repair a backup, run `config validate`, then restart it. Atomic writes use a temporary file, `fsync`, and rename.
