ARCH      := "`uname -s`"
LINUX     := "linux"
MAC       := "Darwin"

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
