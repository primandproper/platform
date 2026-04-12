#!/usr/bin/env bash
set -euo pipefail

# Pre-pull every Docker image used by testcontainers-go across the test suite.
# Pulling up front (in parallel) keeps first-run image-pull time from eating
# into per-test wait-strategy deadlines and lets subsequent runs hit a warm
# cache.
#
# Skipped when RUN_CONTAINER_TESTS is not "true" (nothing to pre-pull if the
# container tests aren't going to run) or when docker isn't on PATH.
#
# Usage: pull_test_containers.sh

RUN_CONTAINER_TESTS="${RUN_CONTAINER_TESTS:-true}"
RUN_CONTAINER_TESTS_LC="$(printf '%s' "${RUN_CONTAINER_TESTS}" | tr '[:upper:]' '[:lower:]')"

if [[ "${RUN_CONTAINER_TESTS_LC}" != "true" ]]; then
	echo "pull_test_containers: RUN_CONTAINER_TESTS != true, skipping"
	exit 0
fi

if ! command -v docker &>/dev/null; then
	echo "pull_test_containers: docker not found on PATH, skipping"
	exit 0
fi

# Keep this list in sync with image literals passed to testcontainers-go
# *.Run / ContainerRequest{Image:...} in *_test.go files. Pulling
# "redis:7-bullseye" also warms "docker.io/redis:7-bullseye" since they
# resolve to the same manifest.
IMAGES=(
	"postgres:17-alpine"
	"mariadb:11"
	"redis:7-bullseye"
	"pgvector/pgvector:pg17"
	"qdrant/qdrant:v1.13.0"
	"gcr.io/google.com/cloudsdktool/cloud-sdk:emulators"
)

# elasticsearch:8.x crashes with SIGILL inside its bundled JDK on linux/arm64
# under Docker Desktop, so TestElasticsearch_Container skips itself on arm64.
# Only pre-pull the image on hosts that will actually run the test.
ARCH="$(uname -m)"
if [[ "${ARCH}" != "arm64" && "${ARCH}" != "aarch64" ]]; then
	IMAGES+=("elasticsearch:8.10.2")
fi

echo "pull_test_containers: pulling ${#IMAGES[@]} images in parallel"

pids=()
for img in "${IMAGES[@]}"; do
	(
		if out=$(docker pull --quiet "$img" 2>&1); then
			echo "  ok  $img"
		else
			echo "  err $img"
			echo "      ${out//$'\n'/$'\n      '}"
			exit 1
		fi
	) &
	pids+=($!)
done

failed=0
for pid in "${pids[@]}"; do
	if ! wait "$pid"; then
		failed=$((failed + 1))
	fi
done

if (( failed > 0 )); then
	echo "pull_test_containers: $failed image(s) failed to pull" >&2
	exit 1
fi

echo "pull_test_containers: done"
