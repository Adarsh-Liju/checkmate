# Simple Makefile for a Go (Gin) app
APP ?= checkmate
PKG ?= $(APP)
BIN_DIR ?= ./bin
GO ?= go

.PHONY: all build run test fmt tidy clean

all: build

build:
	@echo "==> building $(APP)"
	$(GO) build -o $(BIN_DIR)/$(APP) $(PKG)

run:
	@echo "==> running"
	$(GO) run $(PKG)

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

clean:
	@rm -rf $(BIN_DIR)
	@echo "cleaned"

