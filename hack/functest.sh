#!/bin/bash

GOBIN="${GOBIN:-$GOPATH/bin}"
GINKGO=$GOBIN/ginkgo

if ! [ -x "$GINKGO" ]; then
	# The golang-1.13 image used in OpenShift CI enforces vendoring and go get is disabled.
	# Workaround that by unsetting GOFLAGS.
	export GOFLAGS=""
	echo "Retrieving ginkgo and gomega build dependencies"
	go get github.com/onsi/ginkgo/ginkgo
	go get github.com/onsi/gomega/...
else
	echo "GINKO binary found at $GINKGO"
fi

"$GOBIN"/ginkgo build functests/
# Run functional tests
"$GOBIN"/ginkgo -v --slowSpecThreshold=500 --randomizeAllSpecs  --progress functests
