VERSION ?= 0.4.0
CHANNEL ?= dev
DERIVED_DATA ?= /tmp/processgod-derived

export GOCACHE ?= /tmp/gocache
export GOMODCACHE ?= /tmp/gomodcache

.PHONY: test test-go test-swift test-ui build cli package clean

test: test-go test-swift

test-go:
	mkdir -p $(GOCACHE) $(GOMODCACHE)
	go test ./...

test-swift:
	xcodebuild test -project macos/ProcessGodMac.xcodeproj -scheme ProcessGodMac -configuration Debug -derivedDataPath $(DERIVED_DATA) -destination 'platform=macOS,arch=arm64' CODE_SIGNING_ALLOWED=NO -only-testing:ProcessGodMacTests

test-ui:
	xcodebuild test -project macos/ProcessGodMac.xcodeproj -scheme ProcessGodMac -configuration Debug -derivedDataPath $(DERIVED_DATA) -destination 'platform=macOS,arch=arm64' -only-testing:ProcessGodMacUITests

build:
	xcodebuild -project macos/ProcessGodMac.xcodeproj -scheme ProcessGodMac -configuration Release -derivedDataPath $(DERIVED_DATA) CODE_SIGNING_ALLOWED=NO MARKETING_VERSION=$(VERSION) PROCESSGOD_RELEASE_CHANNEL=$(CHANNEL) build
	mkdir -p dist
	rm -rf dist/ProcessGodMac.app
	ditto $(DERIVED_DATA)/Build/Products/Release/ProcessGodMac.app dist/ProcessGodMac.app

cli:
	mkdir -p $(GOCACHE) $(GOMODCACHE) dist
	GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)-$(CHANNEL)" -o dist/processgod-mac ./cmd/processgod

package:
	./scripts/package-dmg.sh $(VERSION) $(CHANNEL)

clean:
	rm -rf dist $(DERIVED_DATA)
