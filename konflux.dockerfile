FROM registry.ci.openshift.org/openshift/release:golang-1.21 AS builder

# renovate: datasource=repology depName=homebrew/openshift-cli
ARG OC_VERSION=4.14.8
# renovate: datasource=github-releases depName=stedolan/jq
ARG JQ_VERSION=1.6
# renovate: datasource=github-releases depName=mikefarah/yq
ARG YQ_VERSION=4.43.1

WORKDIR /github.com/redhat-appstudio/e2e-tests
USER root

COPY go.mod .
COPY go.sum .
RUN go mod download -x
COPY cmd/ cmd/
COPY magefiles/ magefiles/
COPY pkg/ pkg/
COPY tests/ tests/
COPY Makefile .

RUN make build

RUN curl -L "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/${OC_VERSION}/openshift-client-linux.tar.gz" -o /tmp/openshift-client-linux.tar.gz && \
    tar --no-same-owner -xzf /tmp/openshift-client-linux.tar.gz && \
    mv oc kubectl /usr/local/bin && \
    oc version --client && \
    kubectl version --client

RUN curl -L "https://github.com/stedolan/jq/releases/download/jq-${JQ_VERSION}/jq-linux64" -o /usr/local/bin/jq  && \
    chmod +x /usr/local/bin/jq && \
    jq --version

RUN curl -L "https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/yq_linux_amd64" -o /usr/local/bin/yq && \
    chmod +x /usr/local/bin/yq && \
    yq --version

FROM registry.access.redhat.com/ubi8/go-toolset:1.21.9-3.1718100004

ENV GOBIN=$GOPATH/bin

COPY --from=builder /usr/local/bin/jq /usr/local/bin/jq
COPY --from=builder /usr/local/bin/yq /usr/local/bin/yq
COPY --from=builder /usr/local/bin/oc /usr/local/bin/oc
COPY --from=builder /usr/local/bin/kubectl /usr/local/bin/kubectl
COPY --from=builder /github.com/konflux-ci/e2e-tests/bin/e2e-appstudio ./
COPY --from=builder /github.com/konflux-ci/e2e-tests/tests ./tests
