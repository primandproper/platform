# A thin repo-owned rebuild of the published gremlins image.
#
# Upstream is `FROM golang:1.25` with the release binary copied into /usr/bin
# and `USER nonroot`. Two consequences break the run here rather than degrade
# it, and both are properties of the base image rather than of gremlins:
#
#   - This module is `go 1.26`, so the in-image toolchain would fetch 1.26 on
#     first use under GOTOOLCHAIN=auto — a download per run, and an outright
#     failure anywhere GOTOOLCHAIN=local is pinned.
#   - `-o` writes the report into the bind-mounted working directory, which is
#     owned by the host user and not by `nonroot`.
#
# Since the upstream image is nothing but that binary dropped onto a golang
# base, rebuild it on the base this repo already pins as GO_IMAGE and copy the
# same binary across. Leaving USER unset means root, which is how the linter
# container already runs.
ARG GREMLINS_IMAGE=gogremlins/gremlins:0.6.0
ARG GO_IMAGE=golang:1.26-trixie

FROM ${GREMLINS_IMAGE} AS upstream

FROM ${GO_IMAGE}

COPY --from=upstream /usr/bin/gremlins /usr/bin/gremlins

CMD ["gremlins"]
