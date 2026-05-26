#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

required_vars=(
  AWS_REGION
  ECR_REPOSITORY_URL
  IMAGE_TAG
  DOCKER_PLATFORM
)

for var in "${required_vars[@]}"; do
  if [[ -z "${!var:-}" ]]; then
    echo "Missing required environment variable: $var" >&2
    echo "Create .env from .env.example and set the value." >&2
    exit 1
  fi
done

ECR_REGISTRY="${ECR_REPOSITORY_URL%%/*}"
IMAGE_URI="${ECR_REPOSITORY_URL}:${IMAGE_TAG}"

aws ecr get-login-password --region "$AWS_REGION" \
  | docker login --username AWS --password-stdin "$ECR_REGISTRY"

docker build --platform "$DOCKER_PLATFORM" -t "$IMAGE_URI" .
docker push "$IMAGE_URI"

echo "$IMAGE_URI"
