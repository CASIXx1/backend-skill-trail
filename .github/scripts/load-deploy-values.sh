#!/usr/bin/env bash
set -euo pipefail

add_mask() {
  local value="${1:-}"
  if [[ -n "$value" ]]; then
    echo "::add-mask::${value}"
  fi
}

require_value() {
  local name="$1"
  local value="$2"
  if [[ -z "$value" ]]; then
    echo "Missing required value: ${name}" >&2
    exit 1
  fi
}

mask_json_string_values() {
  local json="$1"
  jq -r 'to_entries[] | select(.value | type == "string") | .value' <<< "$json" \
    | while IFS= read -r value; do
        add_mask "$value"
      done
}

csv_to_json_array() {
  local value="$1"
  jq -Rnc --arg value "$value" \
    '$value | split(",") | map(gsub("^\\s+|\\s+$"; "")) | map(select(length > 0))'
}

mask_json_array_values() {
  local json="$1"
  jq -r '.[]' <<< "$json" \
    | while IFS= read -r value; do
        add_mask "$value"
      done
}

normalize_assign_public_ip() {
  local value="$1"
  local normalized
  normalized="$(printf '%s' "$value" | tr '[:lower:]' '[:upper:]')"
  case "$normalized" in
    ENABLED | TRUE | 1 | YES)
      echo "ENABLED"
      ;;
    DISABLED | FALSE | 0 | NO)
      echo "DISABLED"
      ;;
    *)
      echo "Invalid ASSIGN_PUBLIC_IP value. Use ENABLED or DISABLED." >&2
      exit 1
      ;;
  esac
}

tfstate_path="/tmp/terraform.tfstate"
if [[ -n "${TFSTATE_URL:-}" ]]; then
  tfstate_url="$TFSTATE_URL"
else
  require_value "TF_STATE_BUCKET" "${TF_STATE_BUCKET:-}"
  require_value "TF_STATE_KEY" "${TF_STATE_KEY:-}"
  tfstate_url="s3://${TF_STATE_BUCKET}/${TF_STATE_KEY}"
fi
aws s3 cp "$tfstate_url" "$tfstate_path" >/dev/null

ecr_repository_url="$(jq -r '.outputs.api_ecr_repository_url.value // empty' "$tfstate_path")"
worker_ecr_repository_url="$(jq -r '.outputs.worker_ecr_repository_url.value // empty' "$tfstate_path")"
migration_ecr_repository_url="$(jq -r '.outputs.migration_ecr_repository_url.value // empty' "$tfstate_path")"
scheduled_log_ecr_repository_url="$(jq -r '.outputs.scheduled_log_ecr_repository_url.value // empty' "$tfstate_path")"
ecs_cluster_name="$(jq -r '.outputs.ecs_cluster_name.value // empty' "$tfstate_path")"
ecs_service_name="$(jq -r '.outputs.api_ecs_service_name.value // empty' "$tfstate_path")"
worker_ecs_service_name="$(jq -r '.outputs.worker_ecs_service_name.value // empty' "$tfstate_path")"
worker_queue_url="$(jq -r '.outputs.worker_queue_url.value // empty' "$tfstate_path")"
worker_log_group_name="$(jq -r '.outputs.worker_log_group_name.value // empty' "$tfstate_path")"
scheduled_log_log_group_name="$(jq -r '.outputs.scheduled_log_log_group_name.value // empty' "$tfstate_path")"
scheduled_log_task_definition_family="$(jq -r '.outputs.scheduled_log_task_definition_family.value // empty' "$tfstate_path")"
scheduled_log_scheduler_name="$(jq -r '.outputs.scheduled_log_scheduler_name.value // empty' "$tfstate_path")"
scheduled_log_ecspresso_env="$(jq -c '.outputs.scheduled_log_ecspresso_env.value // empty' "$tfstate_path")"
migration_ecspresso_env="$(jq -c '.outputs.migration_ecspresso_env.value // empty' "$tfstate_path")"
worker_ecspresso_env="$(jq -c '.outputs.worker_ecspresso_env.value // empty' "$tfstate_path")"

required_outputs=(
  ecr_repository_url
  worker_ecr_repository_url
  migration_ecr_repository_url
  scheduled_log_ecr_repository_url
  ecs_cluster_name
  ecs_service_name
  worker_ecs_service_name
  worker_queue_url
  worker_log_group_name
  scheduled_log_log_group_name
  scheduled_log_task_definition_family
  scheduled_log_scheduler_name
  migration_ecspresso_env
  worker_ecspresso_env
  scheduled_log_ecspresso_env
)

for name in "${required_outputs[@]}"; do
  require_value "$name" "${!name}"
  add_mask "${!name}"
done

sensitive_output_names=(
  api_log_group_name
  database_master_user_secret_arn
  database_name
  database_port
  database_reader_endpoint
  database_writer_endpoint
  external_service_secret_arn
  migration_log_group_name
  scheduled_log_log_group_name
  new_relic_firelens_image
  new_relic_log_endpoint
  worker_log_group_name
  scheduled_log_task_definition_family
  scheduled_log_scheduler_name
  worker_queue_arn
  worker_queue_url
)

for output_name in "${sensitive_output_names[@]}"; do
  value="$(jq -r --arg name "$output_name" '.outputs[$name].value // empty' "$tfstate_path")"
  add_mask "$value"
done

mask_json_string_values "$migration_ecspresso_env"
mask_json_string_values "$worker_ecspresso_env"
mask_json_string_values "$scheduled_log_ecspresso_env"

migration_env_keys=(
  ASSIGN_PUBLIC_IP
  CONTAINER_NAME
  ECS_CLUSTER_NAME
  SECURITY_GROUP_IDS
  SUBNET_IDS
  TASK_EXECUTION_ROLE_ARN
  TASK_ROLE_ARN
)

for key in "${migration_env_keys[@]}"; do
  value="$(jq -r --arg key "$key" '.[$key] // empty' <<< "$migration_ecspresso_env")"
  require_value "migration_ecspresso_env.${key}" "$value"
  add_mask "$value"
  if [[ "$key" == "ASSIGN_PUBLIC_IP" ]]; then
    value="$(normalize_assign_public_ip "$value")"
    add_mask "$value"
  fi
  echo "MIGRATION_${key}=${value}" >> "$GITHUB_ENV"
done

worker_env_keys=(
  ASSIGN_PUBLIC_IP
  CONTAINER_NAME
  ECS_CLUSTER_NAME
  ECS_SERVICE_NAME
  SECURITY_GROUP_IDS
  SUBNET_IDS
  TASK_EXECUTION_ROLE_ARN
  TASK_ROLE_ARN
)

for key in "${worker_env_keys[@]}"; do
  value="$(jq -r --arg key "$key" '.[$key] // empty' <<< "$worker_ecspresso_env")"
  require_value "worker_ecspresso_env.${key}" "$value"
  add_mask "$value"
  if [[ "$key" == "ASSIGN_PUBLIC_IP" ]]; then
    value="$(normalize_assign_public_ip "$value")"
    add_mask "$value"
  fi
  echo "WORKER_${key}=${value}" >> "$GITHUB_ENV"
done

scheduled_log_env_keys=(
  ASSIGN_PUBLIC_IP
  AWS_REGION
  CONTAINER_NAME
  ECS_CLUSTER_NAME
  LOG_GROUP_NAME
  SECURITY_GROUP_IDS
  SUBNET_IDS
  TASK_EXECUTION_ROLE_ARN
  TASK_ROLE_ARN
)

for key in "${scheduled_log_env_keys[@]}"; do
  value="$(jq -r --arg key "$key" '.[$key] // empty' <<< "$scheduled_log_ecspresso_env")"
  require_value "scheduled_log_ecspresso_env.${key}" "$value"
  add_mask "$value"
  if [[ "$key" == "ASSIGN_PUBLIC_IP" ]]; then
    value="$(normalize_assign_public_ip "$value")"
    add_mask "$value"
  fi
  echo "SCHEDULED_LOG_${key}=${value}" >> "$GITHUB_ENV"
done

migration_subnet_ids="$(jq -r '.SUBNET_IDS' <<< "$migration_ecspresso_env")"
migration_security_group_ids="$(jq -r '.SECURITY_GROUP_IDS' <<< "$migration_ecspresso_env")"
migration_subnet_ids_json="$(csv_to_json_array "$migration_subnet_ids")"
migration_security_group_ids_json="$(csv_to_json_array "$migration_security_group_ids")"

mask_json_array_values "$migration_subnet_ids_json"
mask_json_array_values "$migration_security_group_ids_json"
add_mask "$migration_subnet_ids_json"
add_mask "$migration_security_group_ids_json"
add_mask "$tfstate_url"

{
  echo "ECR_REPOSITORY_URL=${ecr_repository_url}"
  echo "WORKER_ECR_REPOSITORY_URL=${worker_ecr_repository_url}"
  echo "MIGRATION_ECR_REPOSITORY_URL=${migration_ecr_repository_url}"
  echo "SCHEDULED_LOG_ECR_REPOSITORY_URL=${scheduled_log_ecr_repository_url}"
  echo "ECS_CLUSTER_NAME=${ecs_cluster_name}"
  echo "ECS_SERVICE_NAME=${ecs_service_name}"
  echo "WORKER_ECS_SERVICE_NAME=${worker_ecs_service_name}"
  echo "WORKER_QUEUE_URL=${worker_queue_url}"
  echo "WORKER_LOG_GROUP_NAME=${worker_log_group_name}"
  echo "SCHEDULED_LOG_LOG_GROUP_NAME=${scheduled_log_log_group_name}"
  echo "TFSTATE_URL=${tfstate_url}"
  echo "CONTAINER_NAME=app"
  echo "CONTAINER_PORT=8080"
  echo "ASSIGN_PUBLIC_IP=DISABLED"
  echo "WORKER_CONTAINER_NAME=worker"
  echo "SCHEDULED_LOG_CONTAINER_NAME=scheduled-log"
  echo "SCHEDULED_LOG_SCHEDULE_NAME=${scheduled_log_scheduler_name}"
  echo "SCHEDULED_LOG_TASK_DEFINITION_FAMILY=${scheduled_log_task_definition_family}"
  echo "SCHEDULED_LOG_TASK_CPU=256"
  echo "SCHEDULED_LOG_TASK_MEMORY=512"
  echo "WORKER_DESIRED_COUNT=1"
  echo "WORKER_WAIT_TIME_SECONDS=20"
  echo "WORKER_MAX_MESSAGES=10"
  echo "WORKER_VISIBILITY_TIMEOUT=30"
  echo "MIGRATION_SUBNET_IDS_JSON=${migration_subnet_ids_json}"
  echo "MIGRATION_SECURITY_GROUP_IDS_JSON=${migration_security_group_ids_json}"
} >> "$GITHUB_ENV"
