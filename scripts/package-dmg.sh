#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C
export LANG=C

VERSION="${1:-0.4.0}"
CHANNEL="${2:-dev}"
PROJECT_NAME="processgod-mac"
APP_NAME="ProcessGodMac"
TEAM_ID="${TEAM_ID:-8WPJLUNLY8}"
SIGNING_IDENTITY="${SIGNING_IDENTITY:-Developer ID Application: philippe gimenez (8WPJLUNLY8)}"
BUILD_NUMBER="${BUILD_NUMBER:-1}"
DMG_NAME="${PROJECT_NAME}-${VERSION}-${CHANNEL}.dmg"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
DERIVED_DIR="${DIST_DIR}/DerivedData"
ARCHIVE_PATH="${DIST_DIR}/${APP_NAME}.xcarchive"
EXPORT_DIR="${DIST_DIR}/export"
APP_PATH="${EXPORT_DIR}/${APP_NAME}.app"
STAGE_DIR="${DIST_DIR}/dmg-stage"
DMG_PATH="${DIST_DIR}/${DMG_NAME}"
EXPORT_OPTIONS="${DIST_DIR}/ExportOptions.plist"

case "${VERSION}" in
  *[!0-9.]*|.*|*..*|*.) echo "VERSION must be numeric, for example 0.4.0" >&2; exit 2 ;;
esac

require_notary_credentials() {
  if [[ -n "${NOTARY_PROFILE:-}" ]]; then return; fi
  : "${ASC_KEY_ID:?Set ASC_KEY_ID or NOTARY_PROFILE for notarization}"
  : "${ASC_ISSUER_ID:?Set ASC_ISSUER_ID or NOTARY_PROFILE for notarization}"
  : "${ASC_KEY_PATH:?Set ASC_KEY_PATH or NOTARY_PROFILE for notarization}"
  [[ -f "${ASC_KEY_PATH}" ]] || { echo "ASC_KEY_PATH does not exist: ${ASC_KEY_PATH}" >&2; exit 2; }
}

notary_submit() {
  local artifact="$1"
  if [[ -n "${NOTARY_PROFILE:-}" ]]; then
    xcrun notarytool submit "${artifact}" --keychain-profile "${NOTARY_PROFILE}" --wait
  else
    xcrun notarytool submit "${artifact}" --key "${ASC_KEY_PATH}" --key-id "${ASC_KEY_ID}" --issuer "${ASC_ISSUER_ID}" --wait
  fi
}

rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}" /tmp/gocache /tmp/gomodcache

echo "==> Go tests"
(cd "${ROOT_DIR}" && GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go test ./...)

echo "==> Swift unit and IPC integration tests"
xcodebuild test \
  -project "${ROOT_DIR}/macos/ProcessGodMac.xcodeproj" \
  -scheme ProcessGodMac \
  -configuration Debug \
  -derivedDataPath "${DERIVED_DIR}" \
  -destination 'platform=macOS,arch=arm64' \
  CODE_SIGNING_ALLOWED=NO \
  -only-testing:ProcessGodMacTests

if [[ "${RUN_UI_TESTS:-0}" == "1" ]]; then
  echo "==> Swift UI tests"
  xcodebuild test \
    -project "${ROOT_DIR}/macos/ProcessGodMac.xcodeproj" \
    -scheme ProcessGodMac \
    -configuration Debug \
    -derivedDataPath "${DERIVED_DIR}" \
    -destination 'platform=macOS,arch=arm64' \
    DEVELOPMENT_TEAM="${TEAM_ID}" \
    -only-testing:ProcessGodMacUITests
fi

cat >"${EXPORT_OPTIONS}" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>method</key><string>developer-id</string>
  <key>signingStyle</key><string>manual</string>
  <key>teamID</key><string>${TEAM_ID}</string>
  <key>signingCertificate</key><string>${SIGNING_IDENTITY}</string>
</dict></plist>
PLIST

echo "==> Developer ID archive"
xcodebuild archive \
  -project "${ROOT_DIR}/macos/ProcessGodMac.xcodeproj" \
  -scheme ProcessGodMac \
  -configuration Release \
  -archivePath "${ARCHIVE_PATH}" \
  -derivedDataPath "${DERIVED_DIR}" \
  -destination 'generic/platform=macOS' \
  ARCHS=arm64 ONLY_ACTIVE_ARCH=NO \
  MARKETING_VERSION="${VERSION}" \
  CURRENT_PROJECT_VERSION="${BUILD_NUMBER}" \
  PROCESSGOD_RELEASE_CHANNEL="${CHANNEL}" \
  DEVELOPMENT_TEAM="${TEAM_ID}" \
  CODE_SIGN_STYLE=Manual \
  CODE_SIGN_IDENTITY="${SIGNING_IDENTITY}"

echo "==> Developer ID export"
xcodebuild -exportArchive \
  -archivePath "${ARCHIVE_PATH}" \
  -exportPath "${EXPORT_DIR}" \
  -exportOptionsPlist "${EXPORT_OPTIONS}"

codesign --verify --deep --strict --verbose=2 "${APP_PATH}"
[[ "$(lipo -archs "${APP_PATH}/Contents/MacOS/ProcessGodMac")" == "arm64" ]]
[[ "$(lipo -archs "${APP_PATH}/Contents/MacOS/processgod-mac")" == "arm64" ]]

if [[ "${SKIP_NOTARIZATION:-0}" != "1" ]]; then
  require_notary_credentials
  echo "==> Notarize and staple app"
  ditto -c -k --keepParent "${APP_PATH}" "${DIST_DIR}/${APP_NAME}.zip"
  notary_submit "${DIST_DIR}/${APP_NAME}.zip"
  xcrun stapler staple "${APP_PATH}"
  xcrun stapler validate "${APP_PATH}"
  spctl --assess --type execute --verbose=2 "${APP_PATH}"
fi

rm -rf "${STAGE_DIR}"
mkdir -p "${STAGE_DIR}"
ditto "${APP_PATH}" "${STAGE_DIR}/${APP_NAME}.app"
ln -s /Applications "${STAGE_DIR}/Applications"
cp "${ROOT_DIR}/README.md" "${STAGE_DIR}/README.md"
hdiutil create -volname "${APP_NAME}" -srcfolder "${STAGE_DIR}" -ov -format UDZO "${DMG_PATH}"

if [[ "${SKIP_NOTARIZATION:-0}" != "1" ]]; then
  echo "==> Notarize and staple DMG"
  notary_submit "${DMG_PATH}"
  xcrun stapler staple "${DMG_PATH}"
  xcrun stapler validate "${DMG_PATH}"
  spctl --assess --type open --context context:primary-signature --verbose=2 "${DMG_PATH}"
fi

(cd "${DIST_DIR}" && shasum -a 256 "${DMG_NAME}" | tee "${DMG_NAME}.sha256")
echo "Release artifact: ${DMG_PATH}"
