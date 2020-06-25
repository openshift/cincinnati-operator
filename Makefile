.PHONY: \
	clean \
	deploy \
	func-test \
	unit-test

clean:
	@echo "Cleaning previous outputs"
	rm functests/functests.test

deploy:
	@echo "Deploying Cincinnati operator"
	hack/deploy.sh

func-test: deploy
	@echo "Running functional test suite"
	hack/functest.sh

unit-test:
	@echo "Executing unit tests"
	go test -v ./pkg/...
