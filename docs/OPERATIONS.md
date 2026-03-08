# ProcessGodMac Operations

## Install / Start / Stop

User mode:

```bash
processgod-mac service install
processgod-mac service status
```

System mode (pre-login):

```bash
sudo processgod-mac service install --system
sudo processgod-mac service status --system
```

Stop/uninstall:

```bash
processgod-mac service stop
processgod-mac service uninstall
sudo processgod-mac service stop --system
sudo processgod-mac service uninstall --system
```

## Runtime Commands

```bash
processgod-mac status
processgod-mac reload
processgod-mac logs <id> --lines 50
processgod-mac dashboard
```

## Config Location

Default:

- `~/Library/Application Support/ProcessGodMac/config.json`

Config includes:

- `pathEnv`: daemon PATH for command lookup
- `items[]`: guarded process items

## Troubleshooting

1. Dashboard unavailable:

- Ensure tray/daemon is running.
- Check `processgod-mac status`.

2. Command not found:

- Update PATH in dashboard PATH editor.
- Or use absolute executable path.

3. Need pre-login startup:

- Install system mode with `--system` and `sudo`.
