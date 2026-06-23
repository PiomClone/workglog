APP := worklog
VERSION := $(shell cat VERSION)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILT_AT := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.builtAt=$(BUILT_AT)

.PHONY: fmt test build install clean release-snapshot

fmt:
	gofmt -w main.go

test:
	go test ./...

build:
	go build -buildvcs=false -ldflags "$(LDFLAGS)" -o $(APP) .

install: build
	mkdir -p ~/.local/bin
	ln -sf $(CURDIR)/$(APP) ~/.local/bin/$(APP)

clean:
	rm -rf $(APP) dist

release-snapshot:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o dist/$(APP)_darwin_arm64 .
	GOOS=darwin GOARCH=amd64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o dist/$(APP)_darwin_amd64 .
	GOOS=linux GOARCH=amd64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o dist/$(APP)_linux_amd64 .
	GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o dist/$(APP)_linux_arm64 .
