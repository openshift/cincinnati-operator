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

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	hack/kustomize_edit_env_vars.sh
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | oc apply -f -

func-test: deploy
	@echo "Running functional test suite"
	go clean -testcache
	go test -timeout 20m -v ./functests/... || (oc -n openshift-update-service adm inspect --dest-dir="$(ARTIFACT_DIR)/inspect" namespace/openshift-update-service customresourcedefinition/updateservices.updateservice.operator.openshift.io updateservice/example; false)

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

KUSTOMIZE_VERSION ?= 5.2.1
CONTROLLER_TOOLS_VERSION ?= v0.13.0
OPERATOR_SDK_VERSION ?= v1.31.0-ocp

.PHONY: controller-gen
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) GOFLAGS="" go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

KUSTOMIZE ?= $(LOCALBIN)/kustomize
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q v$(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"  | bash -s $(KUSTOMIZE_VERSION) $(LOCALBIN)


.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
OPERATOR_SDK_URL ?= https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/operator-sdk/4.15.21/operator-sdk-v1.31.0-ocp-linux-x86_64.tar.gz
###for mac arm64 users:
###OPERATOR_SDK_URL ?= https://mirror.openshift.com/pub/openshift-v4/arm64/clients/operator-sdk/4.15.21/operator-sdk-v1.31.0-ocp-darwin-aarch64.tar.gz
operator-sdk: $(OPERATOR_SDK)
$(OPERATOR_SDK): $(LOCALBIN)
	@if test -x $(LOCALBIN)/operator-sdk && ! $(LOCALBIN)/operator-sdk version | grep -q $(OPERATOR_SDK_VERSION); then \
		echo "$(LOCALBIN)/operator-sdk version is not expected $(OPERATOR_SDK_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/operator-sdk; \
	fi
	test -s $(LOCALBIN)/operator-sdk || (curl -Lso /tmp/operator-sdk.tar.gz $(OPERATOR_SDK_URL) && tar -vxzf /tmp/operator-sdk.tar.gz --strip=2 -C $(LOCALBIN))


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


# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle

# Build the bundle image.
bundle-build:
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: verify-generate
verify-generate: manifests generate fmt vet
	$(MAKE) bundle VERSION=$(VERSION)
	git diff --exit-code -I"createdAt:"


.PHONY: scorecard-test
scorecard-test: operator-sdk
	test -n "$(KUBECONFIG)" || (echo "The environment variable KUBECONFIG must not empty" && false)
	$(OPERATOR_SDK) scorecard bundle -o text --kubeconfig "$(KUBECONFIG)" -n default
