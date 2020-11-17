FROM registry.svc.ci.openshift.org/ocp/builder:rhel-8-golang-1.15-openshift-4.7 AS builder
WORKDIR /go/src/github.com/openshift/cincinnati-operator/
COPY . .
RUN go build -mod=vendor -o /tmp/build/updateservice-operator github.com/openshift/cincinnati-operator

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

COPY --from=builder /tmp/build/updateservice-operator /usr/bin/updateservice-operator
ENTRYPOINT ["/usr/bin/updateservice-operator"]
