.PHONY: \
	clean \
	func-test \
	unit-test

clean:
	@echo "cleaning previous outputs"
	rm functests/functests.test

func-test:
	@echo "Running functional test suite"
	hack/functest.sh

unit-test:
	@echo "Executing unit tests"
	go test -v ./pkg/...
