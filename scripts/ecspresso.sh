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
  ECS_CLUSTER_NAME
  ECS_SERVICE_NAME
  CONTAINER_NAME
  CONTAINER_PORT
  TASK_EXECUTION_ROLE_ARN
  TASK_ROLE_ARN
  SUBNET_IDS_JSON
  SECURITY_GROUP_IDS_JSON
  ASSIGN_PUBLIC_IP
)

missing_vars=()
for var in "${required_vars[@]}"; do
  if [[ -z "${!var:-}" ]]; then
    missing_vars+=("$var")
  fi
done

if (( ${#missing_vars[@]} > 0 )); then
  echo "Missing required environment variables:" >&2
  printf '  - %s\n' "${missing_vars[@]}" >&2
  echo "Create .env from .env.example and set the values." >&2
  exit 1
fi

if [[ "$ASSIGN_PUBLIC_IP" != "ENABLED" && "$ASSIGN_PUBLIC_IP" != "DISABLED" ]]; then
  echo "ASSIGN_PUBLIC_IP must be ENABLED or DISABLED." >&2
  exit 1
fi

for var in SUBNET_IDS_JSON SECURITY_GROUP_IDS_JSON; do
  if ! jq -e 'type == "array" and length > 0 and all(.[]; type == "string" and contains(",") | not)' >/dev/null <<<"${!var}"; then
    echo "$var must be a non-empty JSON array of strings." >&2
    echo "Example: SUBNET_IDS_JSON='[\"subnet-xxxxxxxx\",\"subnet-yyyyyyyy\"]'" >&2
    exit 1
  fi
done

export IMAGE_URI="${ECR_REPOSITORY_URL}:${IMAGE_TAG}"

exec ecspresso "$@" --config ecspresso/ecspresso.yml
