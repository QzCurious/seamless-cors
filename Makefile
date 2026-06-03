.PHONY: build test

build:
	go build -o bin/cors-gateway ./cmd/cors-gateway

test:
	go test ./...
