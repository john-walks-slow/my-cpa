PLUGIN_NAME := my-cpa-stats-plugin
BIN_DIR     := bin

ifeq ($(OS),Windows_NT)
    EXT := .dll
else
    UNAME_S := $(shell uname -s)
    ifeq ($(UNAME_S),Darwin)
        EXT := .dylib
    else
        EXT := .so
    endif
endif

.PHONY: build test lint clean

build:
	CGO_ENABLED=1 go build -buildmode=c-shared -o $(BIN_DIR)/$(PLUGIN_NAME)$(EXT) ./plugin

test:
	go test ./... -race
	node --check plugin/dashboard/web/dist/app.js

lint:
	go vet ./...

clean:
	rm -rf $(BIN_DIR)
