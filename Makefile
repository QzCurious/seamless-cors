.PHONY: build test

build:
	go build -o bin/seamless-cors ./cmd/seamless-cors

test:
	go test ./...
