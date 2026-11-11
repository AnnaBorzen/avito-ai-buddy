.PHONY: help up down build restart logs ps clean
.PHONY: backend frontend test coverage lint fmt check
.PHONY: frontend-install frontend-build
.PHONY: db-up db-down db-logs

.DEFAULT_GOAL := help

COMPOSE := docker compose

# --- Help ---
help:
	@echo "Available commands:"
	@echo "  make up               - Start all services"
	@echo "  make down             - Stop all services"
	@echo "  make build            - Build containers"
	@echo "  make restart          - Restart services"
	@echo "  make logs             - Show logs"
	@echo "  make ps               - Show containers"
	@echo "  make clean            - Remove containers and volumes"
	@echo "  make backend          - Run backend locally"
	@echo "  make frontend         - Run frontend locally"
	@echo "  make test             - Run backend tests with coverage"
	@echo "  make coverage         - Show backend coverage report"
	@echo "  make lint             - Run Go vet and gofmt check"
	@echo "  make fmt              - Format Go code"
	@echo "  make check            - Run backend and frontend checks"
	@echo "  make frontend-install - Install frontend dependencies (npm ci)"
	@echo "  make frontend-build   - Type-check and build frontend"
	@echo "  make db-up            - Start PostgreSQL"
	@echo "  make db-down          - Stop PostgreSQL"
	@echo "  make db-logs          - Show PostgreSQL logs"

# --- Docker ---
up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

build:
	$(COMPOSE) build

restart:
	$(COMPOSE) restart

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps

clean:
	$(COMPOSE) down -v --remove-orphans

# --- Backend ---
ENV_FILE ?= .env

backend:
	@if [ -f "$(ENV_FILE)" ]; then set -a && . "$(abspath $(ENV_FILE))" && set +a; fi; cd backend && go run ./cmd/api

test:
	cd backend && go test ./... -coverprofile=coverage.out

coverage:
	cd backend && go tool cover -func=coverage.out

lint:
	cd backend && go vet ./...
	@test -z "$$(cd backend && gofmt -l .)" || (echo "gofmt found unformatted files" && exit 1)

fmt:
	cd backend && gofmt -w .

# --- Frontend ---
frontend-install:
	cd frontend && npm ci

frontend-build:
	cd frontend && npm run build

frontend:
	cd frontend && npm run dev

check: lint test frontend-build

# --- Database ---
db-up:
	$(COMPOSE) up -d postgres

db-down:
	$(COMPOSE) stop postgres

db-logs:
	$(COMPOSE) logs -f postgres
