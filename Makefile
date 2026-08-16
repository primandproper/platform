# ENVIRONMENT
PWD      := $(shell pwd)
MYSELF   := $(shell id -u)
MY_GROUP := $(shell id -g)

# PATHS
THIS          := github.com/primandproper/platform-go/v11
ARTIFACTS_DIR := artifacts
SCRIPTS_DIR   := .scripts
DOCKER_DIR    := .docker
COVERAGE_OUT  := $(ARTIFACTS_DIR)/coverage.out
GREMLINS_OUT  := $(ARTIFACTS_DIR)/gremlins.json

# COMPUTED
TOTAL_PACKAGE_LIST := $(shell go list $(THIS)/...)

# CONTAINER VERSIONS
LINTER_IMAGE     := golangci/golangci-lint:v2.10.1
SHELLCHECK_IMAGE := koalaman/shellcheck:stable
GO_IMAGE         := golang:1.26-trixie
# gremlins is pre-1.0 and its own README warns that flags and config files change
# across minor releases, so the tag is exact. It is a container image rather than
# an entry in go.mod's tool block because `go get -tool` on it adds 13 indirect
# requires and force-upgrades viper, cobra and cast — setting the floor for two
# consumers to satisfy a CI-only tool.
GREMLINS_IMAGE   := gogremlins/gremlins:0.6.0
# Built locally by the mutate_image target from GREMLINS_IMAGE and GO_IMAGE; see
# $(DOCKER_DIR)/gremlins.Dockerfile for why the published image is not used as-is.
GREMLINS_BUILT_IMAGE := platform-go-gremlins:0.6.0

# COMMANDS
CONTAINER_RUNNER      := docker
RUN_CONTAINER         := $(CONTAINER_RUNNER) run --rm --volume $(PWD):$(PWD) --workdir=$(PWD) --network=host
RUN_CONTAINER_AS_USER := $(RUN_CONTAINER) --user $(MYSELF):$(MY_GROUP)
LINTER                := $(RUN_CONTAINER) $(LINTER_IMAGE) golangci-lint
SQL_GENERATOR         := $(RUN_CONTAINER_AS_USER) $(SQL_GENERATOR_IMAGE)

# Git ref the mutation gate diffs against, and the host directory bind-mounted
# at the container's build cache. RUN_CONTAINER mounts only $(PWD), so without
# this the cache is ephemeral and every mutant recompiles from cold — which is
# exactly the condition under which every mutant times out, since gremlins
# derives each mutant's timeout from the coverage run's elapsed time. It has to
# live outside $(PWD): gremlins copies the whole module to a temp workdir per
# run, and a multi-gigabyte cache inside the tree would be copied with it. CI
# overrides the path with one it can restore between runs.
GREMLINS_DIFF_REF ?= origin/main
GREMLINS_CACHE    ?= $(HOME)/.cache/platform-go-gremlins
# Capped rather than left at the CPU count, because each worker copies the whole
# module and gremlins leaks descriptors doing it. mutation.sh has the arithmetic.
GREMLINS_WORKERS  ?= 4

# Three of these are load-bearing, and each of them breaks the run rather than
# degrading it:
#
#   - The container runs as root, like the linter does, because -o writes the
#     report into the bind-mounted $(PWD).
#   - gremlins shells out to `git diff --merge-base` to resolve --diff, and git
#     refuses to read a repository owned by another user, which is what the
#     bind-mounted tree looks like from inside the container.
#   - The Docker socket is mounted and RUN_CONTAINER_TESTS is off. gremlins
#     gathers coverage by running the whole suite and aborts on any failure, so
#     the two have to agree: the gate does not want testcontainers standing up a
#     database per mutant, but testutils/containers/*.Try deliberately bypasses
#     the RUN_CONTAINER_TESTS gate and its testcontainers call panics outright
#     when no daemon is reachable. Off plus a socket skips the gated tests and
#     lets the handful of ungated ones connect.
#
# GOFLAGS carries a per-package test timeout, which is the floor under a mutant
# that hangs. gremlins' own deadline is derived from the coverage run and scales
# with it, so on a cold cache it lands beyond the workflow's twenty-minute cap
# entirely — and a hung mutant then takes the whole job, reporting none of the
# mutants that did run. `go test` stopping the binary itself makes that a KILLED
# in bounded time, which is the honest verdict: a test suite that cannot finish
# has not passed. Well clear of the slowest package here, which is a few seconds
# clean and half a minute with a mutation making every bounded wait wait it out.
MUTATE := $(RUN_CONTAINER) \
	--volume /var/run/docker.sock:/var/run/docker.sock \
	--volume $(GREMLINS_CACHE):/gocache \
	--env GOCACHE=/gocache/build \
	--env GOMODCACHE=/gocache/mod \
	--env GOFLAGS=-timeout=180s \
	--env RUN_CONTAINER_TESTS=false \
	--env GIT_CONFIG_COUNT=1 \
	--env GIT_CONFIG_KEY_0=safe.directory \
	--env GIT_CONFIG_VALUE_0=$(PWD) \
	$(GREMLINS_BUILT_IMAGE) gremlins

## non-PHONY folders/files

$(ARTIFACTS_DIR):
	@mkdir -p $(ARTIFACTS_DIR)

## PREREQUISITES

.PHONY: setup
setup: $(ARTIFACTS_DIR) revendor

.PHONY: clean_vendor
clean_vendor:
	$(SCRIPTS_DIR)/clean_vendor.sh

vendor:
	$(SCRIPTS_DIR)/vendor.sh

.PHONY: revendor
revendor: clean_vendor vendor

## FORMATTING

.PHONY: format_imports
format_imports:
	$(SCRIPTS_DIR)/format_imports.sh $(THIS) $(PWD)

.PHONY: format_go_fieldalignment
format_go_fieldalignment:
	@$(SCRIPTS_DIR)/format_go_fieldalignment.sh

.PHONY: format_go_tag_alignment
format_go_tag_alignment:
	@$(SCRIPTS_DIR)/format_go_tag_alignment.sh

.PHONY: go_fix
go_fix:
	go fix ./...

.PHONY: goimports
goimports:
	$(SCRIPTS_DIR)/goimports.sh $(PWD)

.PHONY: format_golang
format_golang: go_fix goimports format_imports format_go_fieldalignment format_go_tag_alignment
	@$(SCRIPTS_DIR)/format_golang.sh $(PWD)

.PHONY: format
format: format_golang

.PHONY: fmt
fmt: format

## LINTING

.PHONY: golang_lint
golang_lint:
	@$(SCRIPTS_DIR)/golang_lint.sh $(CONTAINER_RUNNER) $(LINTER_IMAGE) "$(LINTER)"

.PHONY: shellcheck
shellcheck:
	@$(SCRIPTS_DIR)/shellcheck.sh $(CONTAINER_RUNNER) $(SHELLCHECK_IMAGE) $(SCRIPTS_DIR)

.PHONY: lint
lint: golang_lint shellcheck

## GENERATION

.PHONY: generate
generate:
	go generate ./...

## EXECUTION

.PHONY: build
build:
	$(SCRIPTS_DIR)/build.sh $(TOTAL_PACKAGE_LIST)

.PHONY: test
test: $(ARTIFACTS_DIR) vendor
	$(SCRIPTS_DIR)/test.sh

.PHONY: mutate_image
mutate_image:
	@$(CONTAINER_RUNNER) build --quiet \
		--build-arg GREMLINS_IMAGE=$(GREMLINS_IMAGE) \
		--build-arg GO_IMAGE=$(GO_IMAGE) \
		--tag $(GREMLINS_BUILT_IMAGE) \
		--file $(DOCKER_DIR)/gremlins.Dockerfile $(DOCKER_DIR)

# Mutation testing, scoped to the lines this branch changes. vendor is a real
# prerequisite and not a convenience: vendor/ is gitignored and gremlins' own
# coverage gathering fails outright on inconsistent vendoring.
.PHONY: mutate
mutate: $(ARTIFACTS_DIR) vendor mutate_image
	@mkdir -p $(GREMLINS_CACHE)/build $(GREMLINS_CACHE)/mod
	@GREMLINS_WORKERS=$(GREMLINS_WORKERS) $(SCRIPTS_DIR)/mutation.sh "$(MUTATE)" $(GREMLINS_DIFF_REF) $(GREMLINS_OUT)

.PHONY: bench
bench:
	$(SCRIPTS_DIR)/benchmark.sh
