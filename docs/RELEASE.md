# Release Process

## Prerequisites

- Xcode 16.3 or newer and Swift 6.1
- Go toolchain supporting macOS arm64
- `Developer ID Application: philippe gimenez (8WPJLUNLY8)` in the signing keychain
- App Store Connect API key with notarization access

Secrets are environment variables and must not be committed:

```bash
export ASC_KEY_ID=XXXXXXXXXX
export ASC_ISSUER_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
export ASC_KEY_PATH=/secure/path/AuthKey_XXXXXXXXXX.p8
```

For GitHub Actions, configure `DEVELOPER_ID_P12_BASE64`, `DEVELOPER_ID_P12_PASSWORD`, `BUILD_KEYCHAIN_PASSWORD`, `ASC_PRIVATE_KEY`, `ASC_KEY_ID`, and `ASC_ISSUER_ID` as repository Actions secrets. Then run the **Release DMG** workflow; it publishes the prerelease only after notarization and validation succeed.

## Build

```bash
./scripts/package-dmg.sh 0.4.0 dev
```

The script runs Go tests and Swift unit/real-socket IPC tests, archives arm64 Release with Hardened Runtime, exports with Developer ID, verifies nested signatures, notarizes and staples the app, creates the drag-to-Applications DMG, notarizes and staples it, runs Gatekeeper checks, and emits SHA-256.

Set `RUN_UI_TESTS=1` on a machine with a Mac Development identity to include XCUITest. `SKIP_NOTARIZATION=1` is only for local packaging and must never be used for a GitHub release.

Expected outputs:

- `dist/processgod-mac-0.4.0-dev.dmg`
- `dist/processgod-mac-0.4.0-dev.dmg.sha256`

## GitHub

Tag `v0.4.0-dev`, push the tested commit and tag, then upload both output files to the GitHub release. Do not publish if `stapler validate` or either `spctl` assessment fails.
