FROM registry.ci.openshift.org/openshift/release:golang-1.18 AS builder
WORKDIR /go/src/github.com/openshift/cincinnati-operator/
COPY . .
RUN make GOBUILDFLAGS=-mod=vendor OPERATOR_VERSION="$(git describe --abbrev=8 --dirty --always)" build

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

COPY --from=builder /go/src/github.com/openshift/cincinnati-operator/update-service-operator /usr/bin/update-service-operator
ENTRYPOINT ["/usr/bin/update-service-operator"]
