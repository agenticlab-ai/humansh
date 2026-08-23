.PHONY: build test test-architecture test-classifier bench-classifier test-race test-integration test-zsh test-bash lint install uninstall verify

build:
	go build -o humansh ./cmd/humansh

test:
	go test ./...

test-architecture:
	sh ./scripts/check-architecture.sh
	go test ./internal/app ./internal/bootstrap ./internal/llm/... ./internal/shell/...

test-classifier:
	go test ./internal/classifier ./internal/app
	go test ./internal/classifier -run 'Test|Fuzz' -fuzz=FuzzClassifier -fuzztime=2s

bench-classifier:
	go test ./internal/classifier ./tests/performance -run '^$$' -bench 'Benchmark(Classifier|LiteralProcessStartup)' -benchmem

test-race:
	go test -race ./...

test-integration:
	go test ./internal/llm/... ./internal/app/... ./internal/config/...

test-zsh:
	go test ./internal/shell/zsh/... ./tests/zsh/...

test-bash:
	go test ./internal/shell/bash/... ./tests/bash/...

lint:
	test -z "$$(gofmt -l .)"
	go vet ./...
	@if command -v shellcheck >/dev/null 2>&1; then shellcheck -s sh scripts/*.sh && shellcheck -s bash assets/shell/zsh/humansh.zsh assets/shell/bash/humansh.bash; else echo 'shellcheck not installed; CI must run it'; fi
	zsh -n assets/shell/zsh/humansh.zsh
	bash -n assets/shell/bash/humansh.bash

install:
	sh ./scripts/install.sh --local

uninstall:
	sh ./scripts/uninstall.sh

verify: lint test-architecture test-classifier test test-race test-zsh test-bash
