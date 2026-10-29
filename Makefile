.PHONY: build tidy test dry-run login groups run

GOCACHE ?= $(CURDIR)/.gocache

build:
	GOCACHE="$(GOCACHE)" CGO_ENABLED=0 go build -o bin/job-alert ./cmd/job-alert

tidy:
	go mod tidy

test:
	GOCACHE="$(GOCACHE)" CGO_ENABLED=0 go test ./...

dry-run:
	GOCACHE="$(GOCACHE)" CGO_ENABLED=0 go run ./cmd/job-alert dry-run --config config.json

login:
	GOCACHE="$(GOCACHE)" CGO_ENABLED=0 go run ./cmd/job-alert login --config config.json

groups:
	GOCACHE="$(GOCACHE)" CGO_ENABLED=0 go run ./cmd/job-alert groups --config config.json

run:
	GOCACHE="$(GOCACHE)" CGO_ENABLED=0 go run ./cmd/job-alert run --config config.json
