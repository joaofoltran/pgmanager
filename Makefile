BINARY := pgmanager
PKG := ./cmd/pgmanager

# Detect JS package manager: npm → bun
JS_PM := $(shell command -v npm 2>/dev/null && echo npm || (command -v bun 2>/dev/null && echo bun))
JS_INSTALL := $(if $(filter npm,$(JS_PM)),npm ci,bun install --frozen-lockfile)
JS_RUN := $(if $(filter npm,$(JS_PM)),npm run,bun run)

.PHONY: build test test-integration test-benchmark test-stop setup-bench setup-bench-down lint clean web-install web-build web-dev build-full docker install dev dev-db

build:
	go build -o $(BINARY) $(PKG)

test:
	go test ./... -v

CONTAINER_RT := $(shell command -v docker 2>/dev/null || command -v podman 2>/dev/null)
COMPOSE_CMD := $(shell \
	if command -v docker >/dev/null 2>&1; then echo "docker compose"; \
	elif command -v podman-compose >/dev/null 2>&1; then echo "podman-compose"; \
	elif command -v podman >/dev/null 2>&1; then echo "podman compose"; \
	fi)

test-integration:
ifndef CONTAINER_RT
	$(error No container runtime found. Install docker or podman.)
endif
	@rc=0; \
	cleanup() { echo ""; echo "Tearing down test containers..."; $(COMPOSE_CMD) -f docker-compose.test.yml down -v; exit $$rc; }; \
	trap cleanup EXIT INT TERM; \
	$(COMPOSE_CMD) -f docker-compose.test.yml up -d --wait && \
	go test -tags=integration -v -count=1 -timeout=300s $(if $(RUN),-run=$(RUN)) ./internal/pipeline/ || rc=$$?

test-benchmark:
ifndef CONTAINER_RT
	$(error No container runtime found. Install docker or podman.)
endif
	@rc=0; \
	cleanup() { echo ""; echo "Tearing down benchmark containers..."; $(COMPOSE_CMD) -f docker-compose.bench.yml down -v; exit $$rc; }; \
	trap cleanup EXIT INT TERM; \
	$(COMPOSE_CMD) -f docker-compose.bench.yml up -d --wait && \
	COMPOSE_FILE=docker-compose.bench.yml go test -tags=benchmark -v -count=1 -timeout=4h $(if $(RUN),-run=$(RUN)) ./internal/pipeline/ || rc=$$?

test-stop:
	-$(COMPOSE_CMD) -f docker-compose.test.yml down -v 2>/dev/null
	-$(COMPOSE_CMD) -f docker-compose.bench.yml down -v 2>/dev/null

setup-bench:
	@./scripts/setup-bench.sh $(if $(SIZE),--size $(SIZE))

setup-bench-down:
	@./scripts/setup-bench.sh --down

lint:
	go vet ./...

clean:
	rm -f $(BINARY)

# Frontend targets
web-install:
	cd web && $(JS_INSTALL)

web-build: web-install
	cd web && $(JS_RUN) build
	rm -rf internal/server/dist
	mkdir -p internal/server/dist
	cp -r web/dist/* internal/server/dist/

web-dev:
	cd web && $(JS_RUN) dev

# Full build: frontend + Go binary
build-full: web-build build

# Docker / Podman
CONTAINER_BUILD := $(shell command -v docker 2>/dev/null || command -v podman 2>/dev/null)
docker:
	$(CONTAINER_BUILD) build -t pgmanager .

# Install to /usr/local/bin
install: build-full
	cp $(BINARY) /usr/local/bin/

# Dev: start only the DB container, build natively, and run
dev-db:
	@$(COMPOSE_CMD) up db -d --wait

dev: dev-db build
	PGMANAGER_DB_URL="postgres://pgmanager:pgmanager@localhost:5432/pgmanager?sslmode=disable" \
	PGMANAGER_LOG_LEVEL=debug \
		./$(BINARY)
