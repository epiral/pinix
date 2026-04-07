VERSION ?= $(shell git rev-parse --short HEAD)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build build-all deploy-mini deploy-prod clean

build:
	go build -ldflags "$(LDFLAGS)" -o pinixd ./cmd/pinixd
	go build -ldflags "$(LDFLAGS)" -o pinix ./cmd/pinix

build-all: build
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/pinixd-linux-amd64 ./cmd/pinixd
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/pinix-linux-amd64 ./cmd/pinix
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/pinixd-darwin-arm64 ./cmd/pinixd
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/pinix-darwin-arm64 ./cmd/pinix

deploy-mini:
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/pinixd-darwin-arm64 ./cmd/pinixd
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/pinix-darwin-arm64 ./cmd/pinix
	scp dist/pinixd-darwin-arm64 homefrp:/tmp/pinixd-new
	scp dist/pinix-darwin-arm64 homefrp:/tmp/pinix-new
	ssh homefrp 'launchctl unload ~/Library/LaunchAgents/com.pinix.pinixd.plist 2>/dev/null; sleep 1; cp /tmp/pinixd-new ~/bin/pinixd; cp /tmp/pinix-new ~/bin/pinix; launchctl load ~/Library/LaunchAgents/com.pinix.pinixd.plist; sleep 3; ~/bin/pinixd --version; launchctl list | grep pinixd'

deploy-prod:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/pinixd-linux-amd64 ./cmd/pinixd
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/pinix-linux-amd64 ./cmd/pinix
	scp dist/pinixd-linux-amd64 pinix-prod:/tmp/pinixd-new
	scp dist/pinix-linux-amd64 pinix-prod:/tmp/pinix-new
	ssh pinix-prod 'sudo systemctl stop pinixd 2>/dev/null; sudo cp /tmp/pinixd-new /usr/local/bin/pinixd; sudo cp /tmp/pinix-new /usr/local/bin/pinix; sudo systemctl start pinixd 2>/dev/null; /usr/local/bin/pinixd --version'

clean:
	rm -f pinixd pinix
	rm -rf dist/
