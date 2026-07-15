BIN := bin/wtc

.PHONY: build test lint run fixtures demo

build:
	go build -o $(BIN) ./cmd/wtc

test:
	go test ./...

lint:
	golangci-lint run

run: build
	$(BIN) serve --config ./dev/wtc.yaml

# One-command demo: API + portal UI + seeded fake data (http://localhost:8080).
demo:
	docker compose up --build

fixtures:
	go test ./... -run 'Golden'
