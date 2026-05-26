VERSION ?= $(shell git rev-parse --short HEAD)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build build-all test clean

build:
	go build -ldflags "$(LDFLAGS)" -o pinixd ./cmd/pinixd
	go build -ldflags "$(LDFLAGS)" -o pinix ./cmd/pinix

build-all:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/pinixd-linux-amd64 ./cmd/pinixd
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/pinix-linux-amd64 ./cmd/pinix
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/pinixd-linux-arm64 ./cmd/pinixd
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/pinix-linux-arm64 ./cmd/pinix
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/pinixd-darwin-arm64 ./cmd/pinixd
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/pinix-darwin-arm64 ./cmd/pinix
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/pinixd-windows-amd64.exe ./cmd/pinixd
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/pinix-windows-amd64.exe ./cmd/pinix

test:
	go vet ./...
	go test ./...

upload-cos: build-all
	@echo "Uploading binaries to COS..."
	coscmd upload -H '{"x-cos-acl":"public-read"}' dist/pinixd-linux-amd64 releases/latest/pinixd-linux-amd64
	coscmd upload -H '{"x-cos-acl":"public-read"}' dist/pinix-linux-amd64 releases/latest/pinix-linux-amd64
	coscmd upload -H '{"x-cos-acl":"public-read"}' dist/pinixd-linux-arm64 releases/latest/pinixd-linux-arm64
	coscmd upload -H '{"x-cos-acl":"public-read"}' dist/pinix-linux-arm64 releases/latest/pinix-linux-arm64
	coscmd upload -H '{"x-cos-acl":"public-read"}' dist/pinixd-darwin-arm64 releases/latest/pinixd-darwin-arm64
	coscmd upload -H '{"x-cos-acl":"public-read"}' dist/pinix-darwin-arm64 releases/latest/pinix-darwin-arm64
	coscmd upload -H '{"x-cos-acl":"public-read"}' dist/pinixd-windows-amd64.exe releases/latest/pinixd-windows-amd64.exe
	coscmd upload -H '{"x-cos-acl":"public-read"}' dist/pinix-windows-amd64.exe releases/latest/pinix-windows-amd64.exe
	coscmd upload -H '{"x-cos-acl":"public-read"}' install.sh install.sh
	coscmd upload -H '{"x-cos-acl":"public-read"}' install.ps1 install.ps1
	@echo "Upload complete."
	@echo ""
	@echo "Verify:"
	@echo "  curl -sI https://dl.pinixai.com/install.sh"
	@echo "  curl -sI https://dl.pinixai.com/install.ps1"
	@echo "  curl -sI https://dl.pinixai.com/releases/latest/pinix-darwin-arm64"
	@echo "  curl -sI https://dl.pinixai.com/releases/latest/pinix-linux-amd64"
	@echo "  curl -sI https://dl.pinixai.com/releases/latest/pinix-windows-amd64.exe"

clean:
	rm -f pinixd pinix
	rm -rf dist/
