VERSION ?= $(shell git rev-parse --short HEAD)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build build-all test clean

build:
	go build -ldflags "$(LDFLAGS)" -o pinixd ./cmd/pinixd
	go build -ldflags "$(LDFLAGS)" -o pinix ./cmd/pinix

build-all:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/pinixd-linux-amd64 ./cmd/pinixd
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/pinix-linux-amd64 ./cmd/pinix
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/pinixd-darwin-arm64 ./cmd/pinixd
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/pinix-darwin-arm64 ./cmd/pinix

test:
	go vet ./...
	go test ./...

clean:
	rm -f pinixd pinix
	rm -rf dist/
