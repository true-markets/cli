VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
LDFLAGS := -X github.com/true-markets/defi-cli/internal/cli.Version=$(VERSION) -X github.com/true-markets/defi-cli/internal/cli.CommitSHA=$(COMMIT)

.PHONY: build install test lint fmt generate clean

build:
	go build -ldflags "$(LDFLAGS)" -o defi ./cmd/defi

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/defi

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	gofmt -s -w .
	goimports -w .

generate:
	oapi-codegen -generate types,client -package client -o pkg/client/client.go api/openapi.yaml

clean:
	rm -f defi
	rm -rf dist/
