ARCH      := "`uname -s`"
LINUX     := "linux"
MAC       := "Darwin"

GO           ?= go
GOFMT        ?= $(GO)fmt
FIRST_GOPATH := $(firstword $(subst :, ,$(shell $(GO) env GOPATH)))
GOOPTS       ?=
GOHOSTOS     ?= $(shell $(GO) env GOHOSTOS)
GOHOSTARCH   ?= $(shell $(GO) env GOHOSTARCH)

GO_VERSION        ?= $(shell $(GO) version)
GO_VERSION_NUMBER ?= $(word 3, $(GO_VERSION))
PRE_GO_111        ?= $(shell echo $(GO_VERSION_NUMBER) | grep -E 'go1\.(10|[0-9])\.')
PREFIX                  ?= $(shell pwd)

GOVENDOR :=
GO111MODULE :=
ifeq (, $(PRE_GO_111))
	ifneq (,$(wildcard go.mod))
		# Enforce Go modules support just in case the directory is inside GOPATH (and for Travis CI).
		GO111MODULE := on

		ifneq (,$(wildcard vendor))
			# Always use the local vendor/ directory to satisfy the dependencies.
			GOOPTS := $(GOOPTS) -mod=vendor
		endif
	endif
else
	ifneq (,$(wildcard go.mod))
		ifneq (,$(wildcard vendor))
$(warning This repository requires Go >= 1.11 because of Go modules)
$(warning Some recipes may not work as expected as the current Go runtime is '$(GO_VERSION_NUMBER)')
		endif
	else
		# This repository isn't using Go modules (yet).
		GOVENDOR := $(FIRST_GOPATH)/bin/govendor
	endif
endif
PROMU        := $(FIRST_GOPATH)/bin/promu
pkgs          = ./...

ifeq (arm, $(GOHOSTARCH))
	GOHOSTARM ?= $(shell GOARM= $(GO) env GOARM)
	GO_BUILD_PLATFORM ?= $(GOHOSTOS)-$(GOHOSTARCH)v$(GOHOSTARM)
else
	GO_BUILD_PLATFORM ?= $(GOHOSTOS)-$(GOHOSTARCH)
endif

.PHONY: assets
assets:
	@echo ">> writing assets"
	cd $(PREFIX)/server/ui && GO111MODULE=$(GO111MODULE) $(GO) generate -x -v $(GOOPTS)
	@$(GOFMT) -w ./server/ui

.PHONY: check_assets
check_assets: assets
	@echo ">> checking that assets are up-to-date"
	@if ! (cd $(PREFIX)/server/ui && git diff --exit-code); then \
		echo "Run 'make assets' and commit the changes to fix the error."; \
		exit 1; \
	fi


all:
	@if [ $(ARCH) = $(LINUX) ]; \
	then \
		echo "make in $(LINUX) platform"; \
		GOOS=linux go build -o ./cmd/monitoring  ./cmd/main.go; \
	elif [ $(ARCH) = $(MAC) ]; \
	then \
		echo "make in $(MAC) platform"; \
		GOOS=darwin  go build -o monitoring  ./cmd/main.go; \
	else \
		echo "ARCH unknown"; \
	fi