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

add_mask() {
  local value="${1:-}"
  if [[ -n "$value" ]]; then
    echo "::add-mask::${value}"
  fi
}

require_value "TF_STATE_BUCKET" "${TF_STATE_BUCKET:-}"
require_value "TF_STATE_KEY" "${TF_STATE_KEY:-}"
add_mask "${NEW_RELIC_ACCOUNT_ID:-}"
add_mask "${NEW_RELIC_USER_KEY:-}"

tfstate_path="/tmp/terraform.tfstate"
tfstate_url="s3://${TF_STATE_BUCKET}/${TF_STATE_KEY}"
add_mask "$tfstate_url"
aws s3 cp "$tfstate_url" "$tfstate_path" >/dev/null

alb_dns_name="$(jq -r '.outputs.alb_dns_name.value // empty' "$tfstate_path")"
ecs_cluster_name="$(jq -r '.outputs.ecs_cluster_name.value // empty' "$tfstate_path")"
api_ecs_service_name="$(jq -r '.outputs.api_ecs_service_name.value // empty' "$tfstate_path")"
worker_ecs_service_name="$(jq -r '.outputs.worker_ecs_service_name.value // empty' "$tfstate_path")"
worker_queue_url="$(jq -r '.outputs.worker_queue_url.value // empty' "$tfstate_path")"
worker_log_group_name="$(jq -r '.outputs.worker_log_group_name.value // empty' "$tfstate_path")"

require_value "output.alb_dns_name" "$alb_dns_name"
require_value "output.ecs_cluster_name" "$ecs_cluster_name"
require_value "output.api_ecs_service_name" "$api_ecs_service_name"
require_value "output.worker_ecs_service_name" "$worker_ecs_service_name"
require_value "output.worker_queue_url" "$worker_queue_url"
require_value "output.worker_log_group_name" "$worker_log_group_name"

add_mask "$alb_dns_name"
add_mask "$ecs_cluster_name"
add_mask "$api_ecs_service_name"
add_mask "$worker_ecs_service_name"
add_mask "$worker_queue_url"
add_mask "$worker_log_group_name"

base_url="http://${alb_dns_name}"
add_mask "$base_url"

wait_for_alb() {
  local max_attempts=30
  local attempt=1
  echo "Waiting for ALB to be ready: ${base_url}/health"
  while [[ $attempt -le $max_attempts ]]; do
    if curl -sS -o /dev/null --connect-timeout 5 "${base_url}/health"; then
      echo "ALB is ready."
      return 0
    fi
    echo "Attempt $attempt/$max_attempts: ALB not ready yet, waiting..."
    sleep 10
    attempt=$((attempt + 1))
  done
  echo "ALB did not become ready in time" >&2
  exit 1
}

wait_for_api_service() {
  echo "Waiting for API ECS service to become stable."
  aws ecs wait services-stable \
    --cluster "$ecs_cluster_name" \
    --services "$api_ecs_service_name"
  echo "API ECS service is stable."
}

wait_for_worker_service() {
  echo "Waiting for worker ECS service to become stable."
  aws ecs wait services-stable \
    --cluster "$ecs_cluster_name" \
    --services "$worker_ecs_service_name"
  echo "Worker ECS service is stable."
}

enqueue_worker_job() {
  local max_attempts=12
  local attempt=1

  while [[ $attempt -le $max_attempts ]]; do
    local response_file
    local status
    response_file="$(mktemp)"

    status="$(curl -sS -X POST -o "$response_file" -w '%{http_code}' "${base_url}/worker/jobs")"
    if [[ "$status" == "202" ]]; then
      job_id="$(jq -r '.jobId // empty' "$response_file")"
      message_id="$(jq -r '.messageId // empty' "$response_file")"
      rm -f "$response_file"
      require_value "worker job ID" "$job_id"
      require_value "SQS message ID" "$message_id"
      add_mask "$job_id"
      add_mask "$message_id"
      echo "Worker job enqueued: job_id=${job_id} message_id=${message_id}"
      return 0
    fi

    echo "Attempt $attempt/$max_attempts: /worker/jobs not ready yet (status=${status}), waiting..." >&2
    cat "$response_file" >&2
    echo >&2
    rm -f "$response_file"
    sleep 10
    attempt=$((attempt + 1))
  done

  echo "Worker job endpoint did not return 202 in time" >&2
  exit 1
}

wait_for_queue_to_drain() {
  local max_attempts=30
  local attempt=1
  echo "Waiting for worker queue to drain."
  while [[ $attempt -le $max_attempts ]]; do
    local attrs visible not_visible delayed
    attrs="$(aws sqs get-queue-attributes \
      --queue-url "$worker_queue_url" \
      --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible ApproximateNumberOfMessagesDelayed \
      --output json)"
    visible="$(jq -r '.Attributes.ApproximateNumberOfMessages // "0"' <<< "$attrs")"
    not_visible="$(jq -r '.Attributes.ApproximateNumberOfMessagesNotVisible // "0"' <<< "$attrs")"
    delayed="$(jq -r '.Attributes.ApproximateNumberOfMessagesDelayed // "0"' <<< "$attrs")"

    if [[ "$visible" == "0" && "$not_visible" == "0" && "$delayed" == "0" ]]; then
      echo "Worker queue is drained."
      return 0
    fi

    echo "Attempt $attempt/$max_attempts: queue not drained yet (visible=${visible}, not_visible=${not_visible}, delayed=${delayed}), waiting..."
    sleep 10
    attempt=$((attempt + 1))
  done

  echo "Worker queue did not drain in time" >&2
  exit 1
}

new_relic_nrql_count() {
  local nrql="$1"
  local request_file response_file
  request_file="$(mktemp)"
  response_file="$(mktemp)"

  jq -n \
    --arg query 'query($accountId: Int!, $nrql: Nrql!) { actor { account(id: $accountId) { nrql(query: $nrql) { results } } } }' \
    --argjson accountId "$NEW_RELIC_ACCOUNT_ID" \
    --arg nrql "$nrql" \
    '{query: $query, variables: {accountId: $accountId, nrql: $nrql}}' > "$request_file"

  curl -sS \
    -H "Content-Type: application/json" \
    -H "API-Key: ${NEW_RELIC_USER_KEY}" \
    --data-binary @"$request_file" \
    https://api.newrelic.com/graphql > "$response_file"

  if jq -e '.errors and (.errors | length > 0)' "$response_file" >/dev/null; then
    echo "New Relic NerdGraph returned errors:" >&2
    jq '.errors' "$response_file" >&2
    rm -f "$request_file" "$response_file"
    exit 1
  fi

  jq -r '.data.actor.account.nrql.results[0].count // 0' "$response_file"
  rm -f "$request_file" "$response_file"
}

wait_for_new_relic_worker_log() {
  if [[ -z "${NEW_RELIC_ACCOUNT_ID:-}" || -z "${NEW_RELIC_USER_KEY:-}" ]]; then
    echo "Skipping New Relic Logs check because NEW_RELIC_ACCOUNT_ID or NEW_RELIC_USER_KEY is not set."
    echo "Check New Relic manually with message_id=${message_id} or job_id=${job_id}."
    return 0
  fi

  local escaped_message_id escaped_job_id nrql
  escaped_message_id="${message_id//\'/\\\'}"
  escaped_job_id="${job_id//\'/\\\'}"
  nrql="FROM Log SELECT count(*) AS count WHERE (message_id = '${escaped_message_id}' OR message LIKE '%${escaped_message_id}%' OR message LIKE '%${escaped_job_id}%') SINCE 30 minutes ago"

  local max_attempts=24
  local attempt=1
  echo "Waiting for worker log to appear in New Relic."
  while [[ $attempt -le $max_attempts ]]; do
    local count
    count="$(new_relic_nrql_count "$nrql")"
    if [[ "$count" =~ ^[0-9]+$ && "$count" -gt 0 ]]; then
      echo "New Relic worker log found: count=${count}"
      return 0
    fi

    echo "Attempt $attempt/$max_attempts: New Relic log not found yet, waiting..."
    sleep 15
    attempt=$((attempt + 1))
  done

  echo "New Relic worker log was not found in time" >&2
  echo "NRQL: ${nrql}" >&2
  exit 1
}

wait_for_alb
wait_for_api_service
wait_for_worker_service
enqueue_worker_job
wait_for_queue_to_drain
wait_for_new_relic_worker_log

echo "Worker E2E test completed."
