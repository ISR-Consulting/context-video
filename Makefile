.PHONY: test build run-harness run-live-simulator run-pipeline

test:
	go test ./...

build:
	go build ./...

run-harness:
	go run ./cmd/harness

run-live-simulator:
	go run ./cmd/live-simulator

run-pipeline:
	go run ./cmd/context-pipeline
