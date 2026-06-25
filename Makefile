APP := workglog
VERSION := $(shell cat VERSION)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILT_AT := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.builtAt=$(BUILT_AT)

.PHONY: fmt lint test check build install clean release-snapshot

fmt:
	gofmt -w cmd/workglog/main.go internal/app/app.go

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "golangci-lint is not installed; skipping"; fi

test:
	go test ./...

check: fmt lint test

build:
	go build -buildvcs=false -ldflags "$(LDFLAGS)" -o $(APP) ./cmd/workglog

install: build
	mkdir -p ~/.local/bin
	ln -sf $(CURDIR)/$(APP) ~/.local/bin/$(APP)

clean:
	rm -rf $(APP) worklog dist

release-snapshot:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o dist/$(APP)_darwin_arm64 ./cmd/workglog
	GOOS=darwin GOARCH=amd64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o dist/$(APP)_darwin_amd64 ./cmd/workglog
	GOOS=linux GOARCH=amd64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o dist/$(APP)_linux_amd64 ./cmd/workglog
	GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o dist/$(APP)_linux_arm64 ./cmd/workglog
