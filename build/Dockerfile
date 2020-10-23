FROM docker.io/openshift/origin-release:golang-1.13 AS builder
WORKDIR /go/src/github.com/openshift/cincinnati-operator/
COPY . .
RUN go build -mod=vendor -o /cincinnati-operator github.com/openshift/cincinnati-operator/cmd/manager

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

ENV OPERATOR=/usr/bin/cincinnati-operator \
    USER_UID=1001 \
    USER_NAME=cincinnati-operator

# install operator binary
COPY --from=builder /cincinnati-operator ${OPERATOR}

COPY build/bin /usr/bin
RUN  /usr/bin/user_setup

USER ${USER_UID}
