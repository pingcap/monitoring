ARCH      := "`uname -s`"
LINUX     := "linux"
MAC       := "Darwin"

TARGET_OS ?= $(shell go env GOOS)
TARGET_ARCH ?= $(shell go env ARCH)

export CGO_ENABLED = 0

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
	GOOS=linux go build -o ./reload/build/linux/reload  ./reload/main.go; \
    GOOS=darwin go build -o ./reload/build/macos/reload  ./reload/main.go; \

pull-monitoring:
	go build -o pull-monitoring cmd/monitoring.go

output/dashboards: pull-monitoring
	bash scripts/prepare_dashboards.sh

output/grafana-$(TARGET_OS)-$(TARGET_ARCH).tar.gz : output/dashboards
	bash scripts/build_tiup_grafana.sh
