#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.1.0}"
DEV_TAG="${2:-dev}"
PROJECT_NAME="processgod-mac"
APP_NAME="ProcessGodMac"
DMG_NAME="${PROJECT_NAME}-${VERSION}-${DEV_TAG}.dmg"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
BUILD_BIN="${DIST_DIR}/${PROJECT_NAME}"
APP_DIR="${DIST_DIR}/${APP_NAME}.app"
STAGE_DIR="${DIST_DIR}/dmg-stage"
DMG_PATH="${DIST_DIR}/${DMG_NAME}"

mkdir -p /tmp/gocache /tmp/gomodcache "${DIST_DIR}"
GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go build -ldflags "-X main.version=${VERSION}-${DEV_TAG}" -o "${BUILD_BIN}" ./cmd/processgod

rm -rf "${APP_DIR}" "${STAGE_DIR}" "${DMG_PATH}"
mkdir -p "${APP_DIR}/Contents/MacOS" "${APP_DIR}/Contents/Resources" "${STAGE_DIR}"
cp "${BUILD_BIN}" "${APP_DIR}/Contents/MacOS/${PROJECT_NAME}"
chmod +x "${APP_DIR}/Contents/MacOS/${PROJECT_NAME}"

cat > "${APP_DIR}/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>
  <string>${APP_NAME}</string>
  <key>CFBundleDisplayName</key>
  <string>${APP_NAME}</string>
  <key>CFBundleIdentifier</key>
  <string>com.lovitus.processgod.mac</string>
  <key>CFBundleVersion</key>
  <string>${VERSION}-${DEV_TAG}</string>
  <key>CFBundleShortVersionString</key>
  <string>${VERSION}</string>
  <key>CFBundleExecutable</key>
  <string>${PROJECT_NAME}</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>LSMinimumSystemVersion</key>
  <string>12.0</string>
</dict>
</plist>
PLIST

cp -R "${APP_DIR}" "${STAGE_DIR}/"
cp "${ROOT_DIR}/README.md" "${STAGE_DIR}/README.md"

hdiutil create -volname "${APP_NAME}" -srcfolder "${STAGE_DIR}" -ov -format UDZO "${DMG_PATH}" >/tmp/processgod_dmg.log

echo "DMG created: ${DMG_PATH}"
ls -lh "${DMG_PATH}"
