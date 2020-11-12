FROM docker.io/openshift/origin-release:golang-1.13 AS builder
WORKDIR /go/src/github.com/openshift/cincinnati-operator/
COPY . .
RUN go build -mod=vendor -o /tmp/build/update-service-operator github.com/openshift/cincinnati-operator

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

COPY --from=builder /tmp/build/update-service-operator /usr/bin/update-service-operator
ENTRYPOINT ["/usr/bin/update-service-operator"]
