#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${AWS_ROLE_ARN:-}" ]]; then
  echo "Missing required secret: AWS_ROLE_ARN" >&2
  exit 1
fi
echo "::add-mask::${AWS_ROLE_ARN}"

oidc_response="$(curl -sSf \
  -H "Authorization: bearer ${ACTIONS_ID_TOKEN_REQUEST_TOKEN}" \
  "${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=sts.amazonaws.com")"
oidc_token="$(jq -r '.value' <<< "$oidc_response")"
if [[ -z "$oidc_token" || "$oidc_token" == "null" ]]; then
  echo "Failed to get GitHub OIDC token." >&2
  exit 1
fi
echo "::add-mask::${oidc_token}"

credentials="$(aws sts assume-role-with-web-identity \
  --role-arn "$AWS_ROLE_ARN" \
  --role-session-name "${AWS_ROLE_SESSION_NAME:-github-actions}" \
  --web-identity-token "$oidc_token" \
  --duration-seconds 3600 \
  --query 'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' \
  --output text)"

read -r aws_access_key_id aws_secret_access_key aws_session_token <<< "$credentials"
for value in "$aws_access_key_id" "$aws_secret_access_key" "$aws_session_token"; do
  if [[ -z "$value" ]]; then
    echo "Failed to assume AWS role." >&2
    exit 1
  fi
  echo "::add-mask::${value}"
done

{
  echo "AWS_ACCESS_KEY_ID=${aws_access_key_id}"
  echo "AWS_SECRET_ACCESS_KEY=${aws_secret_access_key}"
  echo "AWS_SESSION_TOKEN=${aws_session_token}"
  echo "AWS_DEFAULT_REGION=${AWS_REGION}"
} >> "$GITHUB_ENV"
