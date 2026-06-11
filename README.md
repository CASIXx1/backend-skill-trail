# api-skill-trail

## ローカル ECR push

`.env.example` からローカル用の `.env` を作成します。

```sh
cp .env.example .env
```

`.env` に以下の値を設定します。

```env
AWS_REGION=<AWS region>
AWS_PROFILE=your-profile-name
ECR_REPOSITORY_URL=123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/backend-skill-trail
IMAGE_TAG=local
DOCKER_PLATFORM=linux/arm64
```

Docker image を build して ECR に push します。

```sh
./scripts/push-image.sh
```

Worker image を同じ script で push する場合は、worker repository URL と Dockerfile を指定します。

```sh
ECR_REPOSITORY_URL="$WORKER_ECR_REPOSITORY_URL" DOCKERFILE=Dockerfile.worker ./scripts/push-image.sh
```

`docker push` が成功すると、push した image URI が表示されます。

## GitHub Actions

| Workflow | Scope |
| --- | --- |
| `ci.yml` | test / vet / Docker build |
| `deploy-backend.yml` | ECR push / ECS deploy |
| `log-test.yml` | API log smoke test |
| `cache-test.yml` | API cache smoke test |
| `worker-e2e-test.yml` | API -> SQS -> worker smoke test and New Relic Logs check |

GitHub Environment `dev` に以下の secrets を設定します。

```env
AWS_REGION=<AWS region>
AWS_ROLE_ARN=<terraform output -raw github_actions_backend_role_arn の値>
TF_STATE_BUCKET=<Terraform remote state bucket>
TF_STATE_KEY=<Terraform remote state key>
NEW_RELIC_ACCOUNT_ID=<New Relic account ID>
NEW_RELIC_USER_KEY=<New Relic NerdGraph user/API key>
```

OIDC trust policy は GitHub Environment `dev` の `sub` claim を許可します。API、migration、worker の ECR repository URL は tfstate の output から取得します。`NEW_RELIC_USER_KEY` はActionsからNerdGraphでNRQLを実行し、worker logがNew Relic Logsへ届いたことを確認するために使います。

## ローカル ecspresso render

`.env` に ECS 用の値を設定します。

必須の ECS 値:

```env
TFSTATE_URL=s3://your-terraform-state-bucket/path/to/terraform.tfstate
ECS_CLUSTER_NAME=your-cluster-name
ECS_SERVICE_NAME=backend-skill-trail
CONTAINER_NAME=app
CONTAINER_PORT=8080
ASSIGN_PUBLIC_IP=DISABLED
```

task definition と service definition は、IAM role、ECR repository URL、private subnet ID、ECS task security group ID、service name などを `TFSTATE_URL` の Terraform state から解決します。

ecspresso の設定と定義ファイルを render します。

```sh
ecspresso --envfile .env --config ecspresso/ecspresso.yml render config
ecspresso --envfile .env --config ecspresso/ecspresso.yml render task-definition
ecspresso --envfile .env --config ecspresso/ecspresso.yml render service-definition
ecspresso --envfile .env --config ecspresso/worker/ecspresso.yml render config
ecspresso --envfile .env --config ecspresso/worker/ecspresso.yml render task-definition
ecspresso --envfile .env --config ecspresso/worker/ecspresso.yml render service-definition
```

現在の ECS 状態との差分を確認します。

```sh
ecspresso --envfile .env --config ecspresso/ecspresso.yml diff
```

ECS service を deploy します。

```sh
ecspresso --envfile .env --config ecspresso/ecspresso.yml deploy
ecspresso --envfile .env --config ecspresso/worker/ecspresso.yml deploy
```

ecspresso v2 では、`deploy` 実行時に ECS service が存在しない場合は作成されます。

ECS service を削除する場合は、先に desired count を `0` にします。

```sh
ecspresso --envfile .env --config ecspresso/ecspresso.yml scale --tasks 0
ecspresso --envfile .env --config ecspresso/ecspresso.yml delete --force
```

## FireLens サイドカー構成

API ECS task は現在、`app` という application container を実行します。FireLens を追加する場合、同じ task 内で 2 つの container を実行します。

```text
infra-skill-trail-dev-api task
  - app
  - log_router
```

関連リポジトリ:

- Backend API / ecspresso 定義: https://github.com/CASIXx1/backend-skill-trail
- Amplify / Next.js フロントエンド: https://github.com/CASIXx1/front-skill-trail
- Terraform インフラ: https://github.com/CASIXx1/infra-skill-trail

### Terraform 側の前提

共通の ECS インフラは Terraform リポジトリで管理します。責務分担は以下です。

- Terraform: ECS cluster、IAM role、VPC、security group、ALB、ECR、NAT Gateway outbound routing、CloudWatch Logs
- ecspresso: ECS service、task definition

FireLens で使う以下の値は tfstate から取得します。

- `output.ecs_task_role_arn`
- `output.ecs_task_execution_role_arn`
- `output.api_ecr_repository_url`
- `output.worker_ecr_repository_url`
- `output.api_log_group_name`
- `output.worker_log_group_name`
- `output.worker_queue_url`
- `output.worker_ecs_service_name`
- `output.private_subnet_ids`

インフラリポジトリには `modules/ecs-logs` があり、`/ecs/${name}/api` という CloudWatch Log Group を作成し、`api_log_group_name` として output します。最初の FireLens 構成では、application log と FireLens sidecar 自身の log を同じ Log Group に出し、stream prefix で分けます。

FireLens から CloudWatch Logs に application log を送るため、ECS task role には CloudWatch Logs への書き込み権限が必要です。

```text
logs:CreateLogStream
logs:PutLogEvents
logs:DescribeLogStreams
```

可能であれば、権限は API 用 Log Group ARN に絞ります。task execution role は image pull と、FireLens sidecar 自身の `awslogs` log driver 用に使います。

API task は private subnet で実行し、`ASSIGN_PUBLIC_IP=DISABLED` のままにします。ECS task の outbound 通信はインフラリポジトリで管理する Regional NAT Gateway 経由です。backend 側の ecspresso 定義では、ECR、CloudWatch Logs、Secrets Manager、SSM、New Relic などの外部 SaaS 送信について VPC Endpoint 前提の設定を置きません。

outbound 通信は NAT 経由なので、FireLens sidecar は `public.ecr.aws/aws-observability/aws-for-fluent-bit:stable` を直接 pull できます。New Relic やその他の外部 HTTPS telemetry 送信も、追加の VPC Endpoint ではなく NAT outbound を使う想定です。

ECS task security group で inbound `24224` port は開けません。FireLens はこの port を内部的に使うため、security group は ALB から application port へ必要な通信だけを許可します。

Worker service 用の ecspresso 定義は `ecspresso/worker` にあります。Worker service も private subnet、public IP なし、Terraform output ベースの network configuration を使います。

## Worker smoke test

API task は `WORKER_QUEUE_URL` が設定されている場合、worker queue へ test job を送信できます。

```sh
curl -X POST https://your-api-domain/worker/jobs
```

成功時は HTTP `202` と `messageId` を返します。`WORKER_QUEUE_URL` が未設定の場合は HTTP `503` を返します。Worker task は SQS を long poll し、受信した message ID/body を構造化ログに出力してから message を削除します。実際の業務処理を追加するときは、処理成功後だけ delete する実装を維持してください。

GitHub Actions の `worker-e2e-test.yml` は、deploy後に以下を確認します。

1. ALB `/health` が応答する。
2. worker ECS service が stable になる。
3. `POST /worker/jobs` が HTTP `202` と `jobId` / `messageId` を返す。
4. worker SQS queue の visible / not visible / delayed message count が `0` になる。
5. New Relic Logs に同じ `messageId` または `jobId` を含むworker logが届く。

### ecspresso 側の変更

`ecspresso/task-def.json` に `firelensConfiguration` を持つ `log_router` container を追加します。

```json
{
  "name": "log_router",
  "image": "public.ecr.aws/aws-observability/aws-for-fluent-bit:stable",
  "essential": true,
  "firelensConfiguration": {
    "type": "fluentbit"
  },
  "logConfiguration": {
    "logDriver": "awslogs",
    "options": {
      "awslogs-group": "{{ tfstate `output.api_log_group_name` }}",
      "awslogs-region": "{{ must_env `AWS_REGION` }}",
      "awslogs-stream-prefix": "firelens"
    }
  }
}
```

`app` container の log driver は `awsfirelens` にします。

```json
"logConfiguration": {
  "logDriver": "awsfirelens",
  "options": {
    "Name": "cloudwatch_logs",
    "region": "{{ must_env `AWS_REGION` }}",
    "log_group_name": "{{ tfstate `output.api_log_group_name` }}",
    "log_stream_prefix": "api/"
  }
}
```

deploy 手順:

1. IAM、CloudWatch Logs、NAT Gateway outbound routing などの Terraform 変更を apply する。
2. `TFSTATE_URL` が更新後の state を指していることを確認する。
3. ecspresso で task definition を render する。
4. ecspresso で deploy する。
5. ECS service が steady state になり、CloudWatch Logs に `api/` と `firelens/` の stream が作成されていることを確認する。
6. task に public IP が付いておらず、必要な外部 HTTPS API に NAT 経由で到達できることを確認する。

deploy 後の確認:

```sh
ecspresso --envfile .env --config ecspresso/ecspresso.yml status
ecspresso --envfile .env --config ecspresso/ecspresso.yml tasks list --output json
aws logs describe-log-streams --log-group-name /ecs/infra-skill-trail-dev/api --order-by LastEventTime --descending
```

期待する結果:

- `app` と `log_router` の ECR image pull が成功している。
- `app` と `log_router` container が running になっている。
- CloudWatch Logs に `api/` と `firelens/` の stream が作成されている。
- 外部 HTTPS API と New Relic への通信が NAT outbound で行われる。
- task ENI が private IP のみを持つ。

参考:

- https://docs.aws.amazon.com/AmazonECS/latest/developerguide/firelens-taskdef.html
- https://docs.aws.amazon.com/AmazonECS/latest/developerguide/using_firelens.html


## ローカル開発環境

Dockerを使用して、Go本体のインストールなしで開発が可能です。

### 起動方法

```bash
docker compose up -d
```

これにより、以下のサービスが起動します：
- アプリケーション (http://localhost:8080)
- PostgreSQL (localhost:5432)
- Redis (localhost:6379)

### マイグレーションの実行

```bash
docker compose run --rm migrate
```

### 動作確認

```bash
curl http://localhost:8080/health
curl http://localhost:8080/db/health
curl http://localhost:8080/cache/health
```
