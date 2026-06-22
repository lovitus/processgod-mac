# Architecture And Security

## Components

- `ProcessGodMac`: Swift 6.1/AppKit/SwiftUI menu bar and window process
- `processgod-mac`: embedded Go CLI and guardian daemon
- `SMAppService.agent`: user LaunchAgent after login
- `SMAppService.daemon`: system LaunchDaemon before login

Swift never reads or writes `config.json`. It uses versioned Unix-socket RPC. Closing Swift has no effect on the daemon lifecycle.

## Configuration

Go owns schema normalization, validation, revisions, and persistence. Every write:

1. checks `expectedRevision` when nonzero
2. clones and validates the next config
3. writes a same-directory temporary file
4. calls `fsync`
5. atomically renames and syncs the directory

`schemaVersion` is currently 2. `revision` is monotonic; exhaustion returns `revision_exhausted` instead of wrapping.

Compatibility-only `minimize` and `noWindow` fields remain in storage. Native process edits preserve them, and scope migration transfers an opaque Go storage config so Swift does not accidentally remove them.

## IPC Security

User sockets are mode `0600`, and the server additionally rejects peer UIDs different from the daemon EUID. System sockets are root/admin `0660`; the server reads Darwin `LOCAL_PEERCRED` and accepts only root or the bootstrapped owner UID after initialization.

Initial system bootstrap is allowed only for a local administrator. The owner UID is stored under `/Library/Application Support/ProcessGodMac/state.json`.

## Logs

Task stdout/stderr is scanned directly into per-task ring buffers. No logger, temporary file, unified-log adapter, or secondary cache contains task output. launchd stdout/stderr for the daemon itself is `/dev/null`.

Error/warning classification includes stderr and lines containing `error`, `warn`, `fatal`, or `panic` case-insensitively. Other stdout uses the standard buffer.

## App Lifecycle

The app normally uses activation policy `accessory`, so it has no Dock icon. Opening manager, logs, or settings changes to `regular`; closing the last window restores `accessory`. The status item remains available throughout.
