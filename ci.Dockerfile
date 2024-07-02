FROM quay.io/konflux-qe-incubator/konflux-qe-tools:latest as builder

FROM registry.access.redhat.com/ubi8/go-toolset:1.21.9-3.1716505664

COPY --from=builder /usr/local/bin/oc /usr/local/bin/oc
COPY --from=builder /usr/local/bin/kubectl /usr/local/bin/kubectl
COPY --from=builder /usr/local/bin/jq /usr/local/bin/jq
COPY --from=builder /usr/local/bin/yq /usr/local/bin/yq

ENV GOBIN=$GOPATH/bin
