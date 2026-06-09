#!/usr/bin/env bash
set -euo pipefail

.github/scripts/validate-aws-env.sh

image_tag="${GITHUB_SHA}"
docker_platform="linux/arm64"
echo "::add-mask::${image_tag}"
echo "::add-mask::${docker_platform}"

{
  echo "IMAGE_TAG=${image_tag}"
  echo "DOCKER_PLATFORM=${docker_platform}"
} >> "$GITHUB_ENV"
