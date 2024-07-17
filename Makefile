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

ARTIFACT_DIR ?= .
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
	go test -timeout 20m -v ./functests/... || (oc -n openshift-updateservice adm inspect --dest-dir="$(ARTIFACT_DIR)/inspect" namespace/openshift-updateservice customresourcedefinition/updateservices.updateservice.operator.openshift.io updateservice/example; false)

unit-test:
	@echo "Executing unit tests"
	go clean -testcache
	go test -v ./controllers/...

build: $(SOURCES)
	go build $(GOBUILDFLAGS) -ldflags="$(GOLDFLAGS)" -o ./update-service-operator ./


.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
CONTROLLER_TOOLS_VERSION ?= v0.13.0

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

kustomize:
ifeq (, $(shell which kustomize))
	@{ \
	set -e ;\
	curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"  | bash -s 5.4.2 $(GOBIN) ;\
	}
KUSTOMIZE=$(GOBIN)/kustomize
else
KUSTOMIZE=$(shell which kustomize)
endif

# Generate manifests e.g. CRD, RBAC etc.
.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=updateservice-operator crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

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
