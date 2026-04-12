BINARY_DIR  := bin
MODULE      := domain-platform
GO          := go
GOFLAGS     := -trimpath

.PHONY: all build server worker migrate agent web test lint \
        migrate-up migrate-down clean dev \
        check-lifecycle-writes check-release-writes check-agent-writes check-agent-safety \
        scanner

all: build

## ── Build ────────────────────────────────────────────────────────────────────
# Default build: control-plane binaries + Pull Agent.
# `scanner` is intentionally NOT in the default target — see ADR-0003 D10.
build: server worker migrate agent

server:
	$(GO) build $(GOFLAGS) -o $(BINARY_DIR)/server ./cmd/server

worker:
	$(GO) build $(GOFLAGS) -o $(BINARY_DIR)/worker ./cmd/worker

migrate:
	$(GO) build $(GOFLAGS) -o $(BINARY_DIR)/migrate ./cmd/migrate

# Pull Agent (Go binary deployed to each Nginx host) — cross-compiled for linux/amd64
agent:
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BINARY_DIR)/agent-linux-amd64 ./cmd/agent

# Parked: cmd/scanner is reserved for the future GFW vertical (ADR-0003 D10).
# Build it manually with `make scanner` only when working on that vertical.
scanner:
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BINARY_DIR)/scanner-linux-amd64 ./cmd/scanner

## ── Test & Lint ──────────────────────────────────────────────────────────────
test:
	$(GO) test ./... -race -timeout 60s

lint:
	golangci-lint run ./...

## ── Database ─────────────────────────────────────────────────────────────────
migrate-up:
	$(BINARY_DIR)/migrate up

migrate-down:
	$(BINARY_DIR)/migrate down

## ── Frontend ─────────────────────────────────────────────────────────────────
web:
	cd web && npm run build

## ── Dev ──────────────────────────────────────────────────────────────────────
dev:
	air -c .air.toml

## ── CI gates (CLAUDE.md Critical Rule #1 / #3) ───────────────────────────────
# Each gate enforces a single-write-path or structural-safety invariant.
# All four MUST pass on every PR. See CLAUDE.md §"Critical Business Rules"
# and DEVELOPMENT_PLAYBOOK.md §2 for the patterns.

# Single write path for domains.lifecycle_state (CLAUDE.md Rule #1)
check-lifecycle-writes:
	@hits=$$(grep -rn 'UPDATE domains SET lifecycle_state' --include='*.go' . | \
		grep -v 'store/postgres/lifecycle.go' | grep -v ':[0-9]*:\s*//' || true); \
	if [ -n "$$hits" ]; then \
		echo "ERROR: direct lifecycle_state writes found outside store/postgres/lifecycle.go:"; \
		echo "$$hits"; exit 1; \
	fi
	@echo "check-lifecycle-writes: OK"

# Single write path for releases.status (CLAUDE.md Rule #1)
check-release-writes:
	@hits=$$(grep -rn 'UPDATE releases SET status' --include='*.go' . | \
		grep -v 'store/postgres/release.go' | grep -v ':[0-9]*:\s*//' || true); \
	if [ -n "$$hits" ]; then \
		echo "ERROR: direct release.status writes found outside store/postgres/release.go:"; \
		echo "$$hits"; exit 1; \
	fi
	@echo "check-release-writes: OK"

# Single write path for agents.status (CLAUDE.md Rule #1)
check-agent-writes:
	@hits=$$(grep -rn 'UPDATE agents SET status' --include='*.go' . | \
		grep -v 'store/postgres/agent.go' | grep -v ':[0-9]*:\s*//' || true); \
	if [ -n "$$hits" ]; then \
		echo "ERROR: direct agents.status writes found outside store/postgres/agent.go:"; \
		echo "$$hits"; exit 1; \
	fi
	@echo "check-agent-writes: OK"

# Structural safety boundary for cmd/agent/ (CLAUDE.md Rule #3)
# The agent binary may only contain hard-coded shell-out points and
# fixed-URL network calls. Variable os/exec, plugin loads, and reflective
# imports are forbidden.
check-agent-safety:
	@hits=$$(grep -rn 'os/exec' cmd/agent/ --include='*.go' | \
		grep -v ':[0-9]*:\s*//' | grep -v '// safe:' || true); \
	if [ -n "$$hits" ]; then \
		echo "WARN: os/exec usage in cmd/agent/ — every call site must be reviewed:"; \
		echo "$$hits"; \
		echo "Each line must either be one of the four hard-coded calls"; \
		echo "(nginx -t, nginx -s reload, configured local-verify HTTP, systemd self-restart)"; \
		echo "OR have a '// safe:' comment with explicit Opus review approval."; \
	fi
	@plugin=$$(grep -rn 'plugin\.Open\|reflect\.Call' cmd/agent/ --include='*.go' | \
		grep -v ':[0-9]*:\s*//' || true); \
	if [ -n "$$plugin" ]; then \
		echo "ERROR: forbidden dynamic loading in cmd/agent/:"; \
		echo "$$plugin"; exit 1; \
	fi
	@echo "check-agent-safety: OK"

## ── Cleanup ──────────────────────────────────────────────────────────────────
clean:
	rm -rf $(BINARY_DIR)
