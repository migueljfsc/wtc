BIN := bin/wtc

.PHONY: build test lint run fixtures

build:
	go build -o $(BIN) ./cmd/wtc

test:
	go test ./...

lint:
	golangci-lint run

run: build
	$(BIN) serve --config ./dev/wtc.yaml

fixtures:
	go test ./... -run 'Golden'
