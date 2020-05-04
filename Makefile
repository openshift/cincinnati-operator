SOURCES := $(shell find . -name '*.go' -not -path "*/vendor/*")

unit-test:
	@echo "Executing unit tests"
	go test -v ./pkg/...

build: $(SOURCES)
	go build -i -ldflags="-s -w" -mod=vendor -o ./cincinnati-operator ./cmd/manager

.PHONY: build \
	unit-test
