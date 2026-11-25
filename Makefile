BINARY := it-kb
CMD := ./cmd/it-kb
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build run test tidy build-linux build-windows clean check-deps install install-system

tidy:
	go mod tidy

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

run:
	CGO_ENABLED=0 go run $(CMD) serve

test:
	CGO_ENABLED=0 go test ./...

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 $(CMD)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 $(CMD)

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe $(CMD)

check-deps:
	./install.sh --check-only --from-source

install:
	./install.sh --from-source --user

install-system:
	./install.sh --from-source --system --service

clean:
	rm -rf bin dist
