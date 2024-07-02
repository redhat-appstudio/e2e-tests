E2E_BIN := ./bin/e2e-appstudio
E2E_ARGS_EXEC ?= ""
CONTAINER_TAG ?= next
CONTAINER_IMAGE_NAME ?= quay.io/redhat-appstudio/e2e:$(CONTAINER_TAG)

export CGO_ENABLED := 0
export GOFLAGS := -mod=mod

build:
	go test -v -c -o $(E2E_BIN) ./cmd/e2e_test.go

build-container:
	podman build -t $(CONTAINER_IMAGE_NAME) .

push-container:
	podman push $(CONTAINER_IMAGE_NAME)

run:
	$(E2E_BIN) $(E2E_ARGS_EXEC)

test/unit:
	go test -v ./pkg/... ./magefiles/...

ci/test/e2e:
	./mage -v ci:teste2e

ci/bootstrap:
	./mage -v ci:bootstrap

ci/test/openshift-upgrade:
	./mage -v ci:performOpenShiftUpgrade

ci/test/upgrade:
	./mage -v ci:testUpgrade 

ci/prepare/e2e-branch:
	./mage -v ci:prepareE2Ebranch

ci/sprayproxy/unregister:
	./mage -v ci:unregisterSprayproxy

local/cluster/prepare:
	./mage -v local:prepareCluster

local/cluster/upgrade:
	./mage -v local:testUpgrade

local/test/e2e:
	./mage -v local:teste2e

local/template/generate-test-suite:
	./mage -v local:generateTestSuiteFile

local/template/generate-test-spec:
	./mage -v local:generateTestSpecFile

clean-gitops-repositories:
	DRY_RUN=false ./mage -v local:cleanupGithubOrg

clean-github-webhooks:
	./mage -v cleanGitHubWebHooks

clean-gitlab-webhooks:
	./mage -v cleanGitLabWebHooks

clean-quay-repos-and-robots:
	./mage -v local:cleanupQuayReposAndRobots

clean-quay-tags:
	./mage -v local:cleanupQuayTags

clean-private-repos:
	./mage -v local:cleanupPrivateRepos

clean-registered-servers:
	./mage -v CleanupRegisteredPacServers

setup-multi-platform-tests:
	./mage -v SetupMultiPlatformTests

setup-multi-arch-tests:
	./mage -v SetupMultiArchTests

setup-source-build:
	./mage -v SetupSourceBuild
