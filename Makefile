SHELL := /bin/bash

.PHONY: help run build build-service test clean release

APP_NAME := ssh-tunnel
SERVICE_NAME := ssh-tunnel-svc
BIN_DIR := bin
PKG := .
SERVICE_PKG := ./service/main
VERSION ?= dev
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GOCACHE ?= /tmp/ssh-tunnel-go-cache
LDFLAGS := -s -w -X ssh-tunnel/buildinfo.Version=$(VERSION) -X ssh-tunnel/buildinfo.BuildTime=$(BUILD_TIME)

UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

ifeq ($(UNAME_S),Darwin)
	GOOS := darwin
endif
ifeq ($(UNAME_S),Linux)
	GOOS := linux
endif

ifeq ($(UNAME_M),x86_64)
	GOARCH := amd64
endif
ifeq ($(UNAME_M),arm64)
	GOARCH := arm64
endif
ifeq ($(UNAME_M),aarch64)
	GOARCH := arm64
endif

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

APP_OUTPUT := $(BIN_DIR)/$(APP_NAME)-$(GOOS)-$(GOARCH)
SERVICE_OUTPUT := $(BIN_DIR)/$(SERVICE_NAME)-$(GOOS)-$(GOARCH)
RUN_CONFIG_ARG := $(if $(CONFIG),--config=$(CONFIG),)
USE_CONFIG_PORTS ?= 0

help:
	@echo "Available targets:"
	@echo "  make run            Run locally with auto-selected free ports (optional: CONFIG=/path/to/config.properties ARGS='...')"
	@echo "  make build          Build local binary to $(APP_OUTPUT)"
	@echo "  make build-service  Build local service binary to $(SERVICE_OUTPUT)"
	@echo "  make test           Run go test ./... with GOCACHE=$(GOCACHE)"
	@echo "  make release        Build multi-platform release artifacts via scripts/build.sh"
	@echo "  make clean          Remove local bin artifacts"
	@echo "  Optional run vars:  LOCAL_ADDRESS=host:port HTTP_ADDRESS=host:port ADMIN_ADDRESS=host:port SSH_PORT=port LOG_FILE=/path/to/log USE_CONFIG_PORTS=1"

run:
	@set -euo pipefail; \
	config_file='$(CONFIG)'; \
	read_config_value() { \
		local key="$$1"; \
		local file="$$2"; \
		if [[ -n "$$file" && -f "$$file" ]]; then \
			sed -n "s/^$${key}[[:space:]]*=[[:space:]]*//p" "$$file" | head -n 1; \
		fi; \
	}; \
	probe_host() { \
		local host="$$1"; \
		case "$$host" in \
			""|"0.0.0.0"|"::"|"*" ) echo "127.0.0.1" ;; \
			*) echo "$$host" ;; \
		esac; \
	}; \
	can_bind_port() { \
		local host="$$1"; \
		local port="$$2"; \
		local check_host="$$(probe_host "$$host")"; \
		if command -v python3 >/dev/null 2>&1; then \
			python3 -c 'import socket, sys; host=sys.argv[1]; port=int(sys.argv[2]); family=socket.AF_INET6 if ":" in host and host != "127.0.0.1" else socket.AF_INET; sock=socket.socket(family, socket.SOCK_STREAM); sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); ok=True; exec("try:\\n sock.bind((host, port))\\nexcept OSError:\\n ok=False"); sock.close(); sys.exit(0 if ok else 1)' "$$check_host" "$$port" >/dev/null 2>&1; \
			return $$?; \
		fi; \
		return 1; \
	}; \
	is_port_in_use() { \
		local host="$$1"; \
		local port="$$2"; \
		local check_host="$$(probe_host "$$host")"; \
		if can_bind_port "$$check_host" "$$port"; then \
			return 1; \
		fi; \
		if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"$$port" -sTCP:LISTEN -t >/dev/null 2>&1; then \
			return 0; \
		fi; \
		if command -v nc >/dev/null 2>&1 && nc -z "$$check_host" "$$port" >/dev/null 2>&1; then \
			return 0; \
		fi; \
		(: >/dev/tcp/"$$check_host"/"$$port") >/dev/null 2>&1; \
	}; \
	find_free_port() { \
		local host="$$1"; \
		local port="$$2"; \
		while is_port_in_use "$$host" "$$port"; do \
			port=$$((port + 1)); \
		done; \
		echo "$$port"; \
	}; \
	resolve_address() { \
		local env_value="$$1"; \
		local config_key="$$2"; \
		local fallback="$$3"; \
		local raw_value="$$env_value"; \
		if [[ -z "$$raw_value" && "$(USE_CONFIG_PORTS)" == "1" ]]; then \
			raw_value="$$(read_config_value "$$config_key" "$$config_file")"; \
		fi; \
		if [[ -z "$$raw_value" ]]; then \
			raw_value="$$fallback"; \
		fi; \
		local host="$${raw_value%:*}"; \
		local port="$${raw_value##*:}"; \
		if [[ -z "$$host" || "$$host" == "$$raw_value" ]]; then \
			host="$${fallback%:*}"; \
		fi; \
		if [[ ! "$$port" =~ ^[0-9]+$$ ]]; then \
			port="$${fallback##*:}"; \
		fi; \
		local free_port="$$(find_free_port "$$host" "$$port")"; \
		echo "$$host:$$free_port"; \
	}; \
	resolve_port() { \
		local env_value="$$1"; \
		local config_key="$$2"; \
		local fallback="$$3"; \
		local raw_value="$$env_value"; \
		if [[ -z "$$raw_value" ]]; then \
			raw_value="$$(read_config_value "$$config_key" "$$config_file")"; \
		fi; \
		if [[ ! "$$raw_value" =~ ^[0-9]+$$ ]]; then \
			raw_value="$$fallback"; \
		fi; \
		echo "$$raw_value"; \
	}; \
		local_address="$$(resolve_address '$(LOCAL_ADDRESS)' 'local.address' '127.0.0.1:18081')"; \
		http_address="$$(resolve_address '$(HTTP_ADDRESS)' 'http.local.address' '127.0.0.1:18082')"; \
		admin_address="$$(resolve_address '$(ADMIN_ADDRESS)' 'admin.address' '127.0.0.1:18083')"; \
		ssh_port="$$(resolve_port '$(SSH_PORT)' 'server.ssh.port' '22')"; \
		log_file='$(LOG_FILE)'; \
		if [[ -z "$$log_file" ]]; then \
			log_file="$$PWD/$(APP_NAME).log"; \
		fi; \
		echo "Using local.address=$$local_address"; \
		echo "Using http.local.address=$$http_address"; \
		echo "Using admin.address=$$admin_address"; \
		echo "Using server.ssh.port=$$ssh_port"; \
		echo "Using log.file.path=$$log_file"; \
		GOCACHE=$(GOCACHE) go run $(PKG) $(RUN_CONFIG_ARG) --local.address="$$local_address" --http.local.address="$$http_address" --admin.address="$$admin_address" --server.ssh.port="$$ssh_port" --log.file.path="$$log_file" $(ARGS)

build:
	mkdir -p $(BIN_DIR)
	GOCACHE=$(GOCACHE) go build -ldflags "$(LDFLAGS)" -o $(APP_OUTPUT) $(PKG)

build-service:
	mkdir -p $(BIN_DIR)
	GOCACHE=$(GOCACHE) go build -ldflags "$(LDFLAGS)" -o $(SERVICE_OUTPUT) $(SERVICE_PKG)

test:
	GOCACHE=$(GOCACHE) go test ./...

release:
	mkdir -p $(BIN_DIR)
	VERSION=$(VERSION) BUILD_TIME="$(BUILD_TIME)" bash scripts/build.sh

clean:
	rm -rf $(BIN_DIR)
