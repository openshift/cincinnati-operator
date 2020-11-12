FROM docker.io/openshift/origin-release:golang-1.13 AS builder
WORKDIR /go/src/github.com/openshift/cincinnati-operator/
COPY . .
RUN go build -mod=vendor -o /tmp/build/updateservice-operator github.com/openshift/cincinnati-operator

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

COPY --from=builder /tmp/build/updateservice-operator /usr/bin/updateservice-operator
ENTRYPOINT ["/usr/bin/updateservice-operator"]
