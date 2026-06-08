#!/usr/bin/env bash
set -euo pipefail

required_secrets=(
  AWS_REGION
  AWS_ROLE_ARN
  TF_STATE_BUCKET
  TF_STATE_KEY
)

for name in "${required_secrets[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required secret: ${name}" >&2
    exit 1
  fi
  echo "::add-mask::${!name}"
done

aws_account_id="$(awk -F: '{print $5}' <<< "$AWS_ROLE_ARN")"
if [[ -n "$aws_account_id" ]]; then
  echo "::add-mask::${aws_account_id}"
fi
