.PHONY: \
	clean \
	deploy \
	func-test

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/images.mk \
	targets/openshift/deps.mk \
	targets/openshift/crd-schema-gen.mk \
)

# $1 - target name
# $2 - apis
# $3 - manifests
# $4 - output
$(call add-crd-gen,cincinnati,./pkg/apis/cincinnati/v1beta1,./pkg/apis/cincinnati/v1beta1,./pkg/apis/cincinnati/v1beta1)

clean:
	@echo "Cleaning previous outputs"
	rm functests/functests.test

deploy:
	@echo "Deploying Cincinnati operator"
	hack/deploy.sh

func-test: deploy
	@echo "Running functional test suite"
	hack/functest.sh
