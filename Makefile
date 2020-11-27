# Current Operator version
VERSION ?= 0.0.1

.PHONY: \
	clean \
	deploy \
	func-test \
	unit-test \
	build

SOURCES := $(shell find . -name '*.go' -not -path "*/vendor/*")
GOBUILDFLAGS ?= -i -mod=vendor
GOLDFLAGS ?= -s -w -X github.com/openshift/cincinnati-operator/version.Operator=$(VERSION)

# This is a placeholder for cincinnati-operator image placeholder
# During development override this when you want to use an specific image
# Example: IMG ?= quay.io/jottofar/updateservice-operator-index:v1
IMG ?= controller:latest

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
	go build $(GOBUILDFLAGS) -ldflags="$(GOLDFLAGS)" -o ./update-service-operator ./
