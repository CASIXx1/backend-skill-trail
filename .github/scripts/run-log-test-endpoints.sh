#!/usr/bin/env bash
set -euo pipefail

require_value() {
  local name="$1"
  local value="${2:-}"
  if [[ -z "$value" ]]; then
    echo "Missing required value: ${name}" >&2
    exit 1
  fi
}

require_value "TF_STATE_BUCKET" "${TF_STATE_BUCKET:-}"
require_value "TF_STATE_KEY" "${TF_STATE_KEY:-}"

tfstate_path="/tmp/terraform.tfstate"
tfstate_url="s3://${TF_STATE_BUCKET}/${TF_STATE_KEY}"
echo "::add-mask::${tfstate_url}"
aws s3 cp "$tfstate_url" "$tfstate_path" >/dev/null

alb_dns_name="$(jq -r '.outputs.alb_dns_name.value // empty' "$tfstate_path")"
require_value "output.alb_dns_name" "$alb_dns_name"
echo "::add-mask::${alb_dns_name}"

base_url="http://${alb_dns_name}"
echo "::add-mask::${base_url}"

wait_for_alb() {
  local max_attempts=30
  local attempt=1
  echo "Waiting for ALB to be ready: ${base_url}/health"
  while [[ $attempt -le $max_attempts ]]; do
    if curl -sS -o /dev/null --connect-timeout 5 "${base_url}/health"; then
      echo "ALB is ready!"
      return 0
    fi
    echo "Attempt $attempt/$max_attempts: ALB not ready yet, waiting..."
    sleep 10
    attempt=$((attempt + 1))
  done
  echo "ALB did not become ready in time" >&2
  exit 1
}

wait_for_alb

call_endpoint() {
  local method="$1"
  local path="$2"
  local expected_status="$3"
  local response_file
  local status

  response_file="$(mktemp)"
  status="$(curl -sS -X "$method" -o "$response_file" -w '%{http_code}' "${base_url}${path}")"

  if [[ "$status" != "$expected_status" ]]; then
    echo "Unexpected status from ${path}: expected ${expected_status}, got ${status}" >&2
    cat "$response_file" >&2
    rm -f "$response_file"
    exit 1
  fi

  test_id="$(jq -r '.testID // empty' "$response_file")"
  if [[ -n "$test_id" ]]; then
    echo "Log test endpoint completed: path=${path} status=${status} test_id=${test_id}"
  else
    echo "Log test endpoint completed: path=${path} status=${status}"
  fi

  rm -f "$response_file"
}

call_endpoint "POST" "/logs/test" "200"
call_endpoint "GET" "/logs/status/ok" "200"
call_endpoint "GET" "/logs/status/error" "500"
call_endpoint "GET" "/logs/ecs" "200"
