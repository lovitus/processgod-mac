# Migration From v0.3.1-dev

On first native launch ProcessGodMac creates `config.json.pre-native` once beside the user config.

For a legacy user LaunchAgent:

1. boot out the old service but keep its plist
2. register the bundled SMAppService agent
3. connect and perform health/config checks
4. delete the old plist only after success
5. unregister the new service and bootstrap the old plist if validation fails

For system mode, the app uses administrator authorization for old LaunchDaemon operations. User-to-system switching exports the complete Go storage config, registers the system daemon, waits for macOS approval, bootstraps the owner UID, imports configuration, checks health, and only then unregisters user mode.

Migrated data includes tasks, PATH, working directory, arguments, environment, modes, enabled state, and compatibility-only `minimize/noWindow` fields.

Do not manually delete the old plist before the native app reports a healthy target daemon; it is the rollback point.
