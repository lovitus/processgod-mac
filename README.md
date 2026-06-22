# ProcessGodMac

Native macOS process guardian derived from [lovitus/processgod](https://github.com/lovitus/processgod).

`v0.4.0-dev` replaces the former tray/web UI with a Swift 6 menu bar app while retaining a Go guardian daemon and compatible configuration storage.

## Requirements

- macOS 15 or newer
- Apple Silicon (`arm64`)
- No browser dashboard and no TCP ports `51089`/`51090`

## Features

- Native `NSStatusItem` popover for status, enable/disable, restart, log preview, and pause/resume all
- SwiftUI process manager with command picker, arguments, working directory, environment variables, mode, and Cron validation
- User LaunchAgent after login or administrator-approved system LaunchDaemon before login
- Always Guard, Start Once, Cron Run, and Cron Restart behavior
- Persistent global pause without unloading launchd
- English and Simplified Chinese UI, following the system or a manual override
- Go CLI and newline-delimited JSON IPC over authenticated Unix sockets
- Atomic revisioned configuration with legacy field preservation
- Strictly bounded, memory-only task logs

## Install

1. Open `processgod-mac-0.4.0-dev.dmg`.
2. Drag `ProcessGodMac.app` to Applications.
3. Open ProcessGodMac. Its icon appears in the menu bar.

A notarized release does not require `xattr`. Fresh installs automatically register the user service. Closing the Swift app does not stop the guardian.

Use **Settings > Before login (System)** for pre-login startup. macOS may require approval in **System Settings > General > Login Items & Extensions**. The user daemon remains active until the system daemon is approved, imported, and healthy.

## Log Limits

Each enabled task owns exactly two in-memory ring buffers:

| Category | Retained lines |
| --- | ---: |
| Error / warning | 100 |
| Standard / other | 20 |

Each stored line is capped at 4096 bytes. Therefore retained text is bounded to at most 491,520 bytes per task, plus fixed Go object/string overhead. The UI and CLI read these same buffers; there is no larger hidden cache and no task log file.

Sequence values are signed 64-bit counters. At the theoretical maximum (`9,223,372,036,854,775,807`) the counter restarts at 1; this does not allocate memory or change ring capacities.

## Build And Test

```bash
make test
make build VERSION=0.4.0 CHANNEL=dev
```

The Xcode project is at `macos/ProcessGodMac.xcodeproj`. The app target builds and embeds an `arm64` Go helper at `Contents/MacOS/processgod-mac`.

## CLI

```bash
/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac status
/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac logs <task-id>
/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac restart <task-id>
/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac pause
/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac resume
```

Add `--system` to connect to the system daemon.

## Documentation

- [User Guide](docs/USER_GUIDE.md)
- [Operations](docs/OPERATIONS.md)
- [Architecture and Security](docs/ARCHITECTURE.md)
- [IPC Protocol](docs/IPC.md)
- [Migration](docs/MIGRATION.md)
- [Release Process](docs/RELEASE.md)

## Release

```bash
ASC_KEY_ID=... \
ASC_ISSUER_ID=... \
ASC_KEY_PATH=/secure/path/AuthKey_XXXX.p8 \
./scripts/package-dmg.sh 0.4.0 dev
```

The script tests, archives, Developer ID signs, exports, notarizes, staples, validates, creates `dist/processgod-mac-0.4.0-dev.dmg`, and writes its SHA-256 file.
