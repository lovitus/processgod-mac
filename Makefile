VERSION ?= 0.1.0
CHANNEL ?= dev

export GOCACHE ?= /tmp/gocache
export GOMODCACHE ?= /tmp/gomodcache

.PHONY: test build package clean

test:
	mkdir -p $(GOCACHE) $(GOMODCACHE)
	go test ./...

build:
	mkdir -p $(GOCACHE) $(GOMODCACHE) dist
	go build -ldflags "-X main.version=$(VERSION)-$(CHANNEL)" -o dist/processgod-mac ./cmd/processgod

package: build
	./scripts/package-dmg.sh $(VERSION) $(CHANNEL)

clean:
	rm -rf dist .runtime
