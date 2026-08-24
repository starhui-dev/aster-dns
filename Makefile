SHELL := /bin/sh
GOFLAGS ?= -buildvcs=false
export GOFLAGS

GO_PACKAGES := ./cmd/... ./internal/... ./migrations
GO_SOURCES := $(shell go list -f '{{range .GoFiles}}{{$$.Dir}}/{{.}} {{end}}{{range .TestGoFiles}}{{$$.Dir}}/{{.}} {{end}}' $(GO_PACKAGES))
IMAGE ?= aster-dns:local

.PHONY: \
	setup format check build ci test \
	backend-format backend-format-check backend-lint backend-test backend-build \
	frontend-install frontend-format frontend-format-check frontend-lint frontend-typecheck frontend-test frontend-build \
	dev-db migrate dev-backend dev-frontend compose-up compose-down container-build

setup: frontend-install

format: backend-format frontend-format

check: backend-format-check backend-lint backend-test frontend-format-check frontend-lint frontend-typecheck frontend-test

build: backend-build frontend-build

ci: check build

test: backend-test frontend-test

backend-format:
	gofmt -w $(GO_SOURCES)

backend-format-check:
	@unformatted="$$(gofmt -l $(GO_SOURCES))"; \
	if [ -n "$$unformatted" ]; then \
		printf 'Go files require formatting:\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

backend-lint:
	go vet $(GO_PACKAGES)

backend-test:
	go test $(GO_PACKAGES)

backend-build:
	go build $(GO_PACKAGES)

frontend-install:
	npm ci --prefix web

frontend-format:
	npm --prefix web run format

frontend-format-check:
	npm --prefix web run format:check

frontend-lint:
	npm --prefix web run lint

frontend-typecheck:
	npm --prefix web run typecheck

frontend-test:
	npm --prefix web run test

frontend-build:
	npm --prefix web run build

dev-db:
	docker compose up -d postgres

migrate:
	go run ./cmd/server migrate up

dev-backend:
	go run ./cmd/server serve

dev-frontend:
	npm --prefix web run dev

compose-up:
	docker compose up --build

compose-down:
	docker compose down

container-build:
	docker build --tag $(IMAGE) .
