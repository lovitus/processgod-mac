# IPC Protocol

Transport is newline-delimited JSON over an `AF_UNIX` stream socket. Protocol version is 1.

Request:

```json
{"protocolVersion":1,"requestID":"uuid","method":"system.health","params":{}}
```

Success:

```json
{"protocolVersion":1,"requestID":"uuid","ok":true,"result":{}}
```

Failure:

```json
{"protocolVersion":1,"requestID":"uuid","ok":false,"error":{"code":"revision_conflict","message":"..."}}
```

Methods:

- `system.hello`, `system.health`, `system.bootstrap`
- `config.get`, `config.export`, `config.import`, `config.reload`
- `process.create`, `process.update`, `process.delete`, `process.setEnabled`, `process.restart`
- `guardian.pause`, `guardian.resume`
- `settings.update`, `status.list`, `logs.snapshot`, `cron.validate`
- `events.subscribe`, `daemon.shutdown`

Mutation methods accept `expectedRevision`. Zero is reserved for trusted migration/CLI operations that intentionally bypass conflict checking.

`events.subscribe` first returns a normal acknowledgement, then event envelopes. Heartbeats are sent every 15 seconds. Swift reconnects at 0.5, 1, 2, 5, and 10 second intervals and reloads complete snapshots after reconnecting.

Stable error codes include `protocol_mismatch`, `invalid_request`, `method_not_found`, `revision_conflict`, `revision_exhausted`, `duplicate_id`, `not_found`, `invalid_cron`, `permission_denied`, `not_bootstrapped`, and `already_bootstrapped`.
