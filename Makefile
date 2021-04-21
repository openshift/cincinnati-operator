# Current Operator version
VERSION ?= 0.0.1

.PHONY: \
	clean \
	deploy \
	func-test \
	unit-test \
	build \
	manifests \
	gen-for-bundle \
	bundle \
	bundle-build

SOURCES := $(shell find . -name '*.go' -not -path "*/vendor/*")
GOBUILDFLAGS ?= -i -mod=vendor
GOLDFLAGS ?= -s -w -X github.com/openshift/cincinnati-operator/version.Operator=$(VERSION)

# This is a placeholder for cincinnati-operator image placeholder
# During development override this when you want to use an specific image
# Example: IMG ?= quay.io/jottofar/update-service-operator:v1
IMG ?= controller:latest

BUNDLE_IMG ?= controller-bundle:latest

CHANNELS ?= v1
BUNDLE_CHANNELS = --channels=$(CHANNELS)

DEFAULT_CHANNEL ?= v1
BUNDLE_DEFAULT_CHANNEL = --default-channel=$(DEFAULT_CHANNEL)

BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

clean:
	@echo "Cleaning previous outputs"
	go clean -testcache
	rm functests/functests.test

deploy:
	@echo "Deploying Update Service operator"
	hack/deploy.sh

func-test: deploy
	@echo "Running functional test suite"
	go clean -testcache
	go test -timeout 20m -v ./functests/...

unit-test:
	@echo "Executing unit tests"
	go clean -testcache
	go test -v ./controllers/...

build: $(SOURCES)
	go build $(GOBUILDFLAGS) -ldflags="$(GOLDFLAGS)" -o ./update-service-operator ./

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.3.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

kustomize:
ifeq (, $(shell which kustomize))
	@{ \
	set -e ;\
	KUSTOMIZE_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$KUSTOMIZE_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/kustomize/kustomize/v3@v3.5.4 ;\
	rm -rf $$KUSTOMIZE_GEN_TMP_DIR ;\
	}
KUSTOMIZE=$(GOBIN)/kustomize
else
KUSTOMIZE=$(shell which kustomize)
endif

# Generate manifests e.g. CRD, RBAC etc.
manifests:  controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=updateservice-operator webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Generate bundle manifests
gen-for-bundle: manifests kustomize
	operator-sdk generate kustomize manifests -q

# Generate bundle and metadata, then validate generated files.
bundle: kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

# Build the bundle image.
bundle-build:
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .
