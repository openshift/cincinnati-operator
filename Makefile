.PHONY: \
	clean \
	deploy \
	func-test \
	unit-test \
	build

SOURCES := $(shell find . -name '*.go' -not -path "*/vendor/*")
IMG ?= quay.io/updateservice/updateservice-operator:latest

clean:
	@echo "Cleaning previous outputs"
	rm functests/functests.test

deploy:
	@echo "Deploying Update Service operator"
	hack/deploy.sh

func-test: deploy
	@echo "Running functional test suite"
	go test -v ./functests/...

unit-test:
	@echo "Executing unit tests"
	go test -v ./controllers/...

build: $(SOURCES)
	go build -i -ldflags="-s -w" -mod=vendor -o ./updateservice-operator ./
