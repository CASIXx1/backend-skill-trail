## 1. Goal
Aurora PostgreSQL の `migrationuser` と Secrets Manager、ecspresso migration task、IAM 権限が一致しており、migration task が正しいDBユーザーで実行できる状態であることを確認・必要時に修正する。

## 2. Approach
まずDB実体を最優先で確認し、`migrationuser` が存在しない場合だけ作成と権限付与を行う。次に Secrets Manager の `/database/migrationuser` の `username` / `password` がDB実体と一致することを確認し、最後に backend の ecspresso render 結果と infra の IAM policy がその secret ARN を参照できることを確認する。backend 側の静的定義では `ecspresso/migration/task-def.json:32-40` が `database_migration_user_secret_arn` を参照し、通常API側の `ecspresso/task-def.json:53-60` は `database_app_user_secret_arn` を参照しているため、コード上の appuser/migrationuser 取り違えは現時点では見当たらない。

## 3. File Changes
- Modify: なし。今回の主作業はAWS/Auroraの運用確認であり、Plan Mode ではファイル変更しない。
- Create: なし。
- Delete: なし。

参照した既存ファイル:
- `/Users/horikawa/GolandProjects/backend-skill-trail/ecspresso/migration/task-def.json:32-40`: migration task の `DB_USER` / `DB_PASSWORD` が `output.database_migration_user_secret_arn` の `username` / `password` を参照している。
- `/Users/horikawa/GolandProjects/backend-skill-trail/ecspresso/task-def.json:53-60`: API task は `output.database_app_user_secret_arn` を参照しており、migration task と分離されている。
- `/Users/horikawa/GolandProjects/backend-skill-trail/.github/scripts/load-deploy-values.sh:60-74`: Terraform state を取得し、deployment に必要な outputs を読む。
- `/Users/horikawa/GolandProjects/backend-skill-trail/.github/scripts/load-deploy-values.sh:108-127`: `migration_ecspresso_env` から `MIGRATION_TASK_EXECUTION_ROLE_ARN` などを GitHub Actions env に展開する。
- `/Users/horikawa/GolandProjects/infra-skill-trail/modules/database/app_user_secret.tf:20-37`: Secrets Manager の migration user secret は `${var.name}/database/migrationuser` で、JSON に `username` / `password` / `engine` / `host` / `port` / `dbname` を含む。
- `/Users/horikawa/GolandProjects/infra-skill-trail/modules/database/variables.tf:46-56`: migration user のデフォルトは `migrationuser` / `migrationpassword`。
- `/Users/horikawa/GolandProjects/infra-skill-trail/modules/containers/iam.tf:56-76`: task execution role に `secretsmanager:GetSecretValue` が付与され、resources に `var.database_migration_user_secret_arn` が含まれる。

## 4. Implementation Steps

### Task 1: Aurora DB の migration user を確認する
1. AWS 認証情報と対象環境を確認する。`AWS_PROFILE`、`AWS_REGION`、Terraform state の環境が dev など対象環境と一致していることを確認する。
2. Terraform output または AWS Console から Aurora writer endpoint、port、database name、master user secret を取得する。backend の migration task は `ecspresso/migration/task-def.json:16-29` で writer endpoint、port、database name を tfstate から参照するため、同じ値を使う。
3. master user で Aurora PostgreSQL に接続する。例: `psql "host=<writer-endpoint> port=<port> dbname=app user=<master-user> sslmode=require"`。
4. 以下を実行してユーザー存在を確認する。

```sql
SELECT usename FROM pg_user WHERE usename = 'migrationuser';
```

5. 結果が1行で `migrationuser` の場合は作成SQLは実行しない。0行の場合のみ以下を実行する。

```sql
CREATE USER migrationuser WITH PASSWORD 'migrationpassword';
GRANT ALL PRIVILEGES ON DATABASE app TO migrationuser;
GRANT ALL ON SCHEMA public TO migrationuser;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO migrationuser;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO migrationuser;
```

6. 作成済みだがパスワード不一致が疑われる場合は、Secrets Manager と一致させる値で `ALTER USER migrationuser WITH PASSWORD '<secret password>';` を実行する。パスワードをログやシェル履歴に残さない方法を使う。

### Task 2: Secrets Manager の migration user secret を確認する
1. `/Users/horikawa/GolandProjects/infra-skill-trail/modules/database/app_user_secret.tf:20-37` の定義どおり、対象 secret 名が `<name>/database/migrationuser` であることを確認する。ユーザー指定の `/database/migrationuser` は suffix として一致する。
2. AWS Console または AWS CLI で secret value を確認する。CLI例: `aws secretsmanager get-secret-value --secret-id <migration-secret-arn-or-name> --query SecretString --output text`。
3. JSON の `username` が `migrationuser`、`password` が DB に設定した値、`dbname` が `app`、`host` が Aurora writer endpoint、`port` が Aurora port と一致することを確認する。
4. 不一致の場合は Secrets Manager の secret value を修正する。Terraform 管理値の場合は、手動修正後に Terraform apply で戻らないよう、`modules/database/variables.tf:46-56` の変数値または環境 tfvars 側の override 方針も確認する。

### Task 3: ecspresso migration task の参照先を render で確認する
1. backend repo で、GitHub Actions と同じ Terraform state を指す `TFSTATE_URL`、`AWS_REGION`、`IMAGE_TAG`、migration 用 env を準備する。GitHub Actions では `.github/scripts/load-deploy-values.sh:60-74` が tfstate を読み、`.github/scripts/load-deploy-values.sh:108-127` が `MIGRATION_*` env を作る。
2. migration task definition を render する。

```sh
ecspresso --envfile .env --config ecspresso/migration/ecspresso.yml render task-definition
```

3. render 結果の secrets が以下になっていることを確認する。

```json
{
  "name": "DB_USER",
  "valueFrom": "<database_migration_user_secret_arn>:username::"
}
{
  "name": "DB_PASSWORD",
  "valueFrom": "<database_migration_user_secret_arn>:password::"
}
```

4. render 結果の ARN が app user secret ではなく migration user secret であることを確認する。静的定義上は `ecspresso/migration/task-def.json:32-40` が migration output、`ecspresso/task-def.json:53-60` が app output を使っている。

### Task 4: IAM task execution role の Secrets Manager 権限を確認する
1. Terraform state の `ecs_task_execution_role_arn` / `ecs_task_execution_role_name` と `database_migration_user_secret_arn` を確認する。
2. infra repo の `modules/containers/iam.tf:56-76` どおり、task execution role policy に `secretsmanager:GetSecretValue` があり、resource に migration user secret ARN が含まれていることを確認する。
3. AWS CLI で実環境の policy も確認する。例: `aws iam list-role-policies --role-name <task-execution-role-name>` と `aws iam get-role-policy --role-name <task-execution-role-name> --policy-name <policy-name>`。
4. policy に migration user secret ARN がない場合は、infra repo で Terraform state/variables の入力が正しいことを確認して Terraform plan/apply を行う。

### Task 5: migration task を最小実行で検証する
1. DB/secret/IAM の整合性確認後、migration verify を実行する。GitHub Actions では `.github/workflows/deploy-backend.yml:98-105` 相当で `MIGRATION_COMMAND=verify` を指定している。
2. 手元から実行する場合は `ecspresso --config ecspresso/migration/ecspresso.yml run --wait` を `MIGRATION_COMMAND=verify` 付きで実行する。
3. CloudWatch Logs の migration log group を確認し、認証エラー、secret fetch エラー、permission denied が出ていないことを確認する。

## 5. Acceptance Criteria
- `SELECT usename FROM pg_user WHERE usename = 'migrationuser';` がちょうど1行 `migrationuser` を返す。
- `migrationuser` で `dbname=app` に接続できる。
- `migrationuser` が `public` schema に対して migration 実行に必要なDDL/DML権限を持つ。最低限、`GRANT ALL ON SCHEMA public TO migrationuser` と、既存 table/sequence への `GRANT ALL PRIVILEGES` が適用済みである。
- Secrets Manager の migration user secret の JSON で `username == "migrationuser"`、`password == DB側のmigrationuser password`、`dbname == "app"`、`port == Aurora port` が成立する。
- `ecspresso/migration/task-def.json` の render 結果で `DB_USER` と `DB_PASSWORD` の `valueFrom` が `database_migration_user_secret_arn` を指し、`database_app_user_secret_arn` を含まない。
- ECS task execution role の実IAM policy に `secretsmanager:GetSecretValue` があり、対象 resource に migration user secret ARN が含まれる。
- `MIGRATION_COMMAND=verify` の migration ECS task が exit code 0 で完了する。
- CloudWatch Logs に `password authentication failed`、`AccessDeniedException`、`ResourceNotFoundException`、`permission denied for schema public` が出ない。

## 6. Verification Steps
- DB確認SQL:

```sql
SELECT usename FROM pg_user WHERE usename = 'migrationuser';
SELECT current_database();
```

- 必要時のDB作成/権限SQL:

```sql
CREATE USER migrationuser WITH PASSWORD 'migrationpassword';
GRANT ALL PRIVILEGES ON DATABASE app TO migrationuser;
GRANT ALL ON SCHEMA public TO migrationuser;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO migrationuser;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO migrationuser;
```

- Secret確認:

```sh
aws secretsmanager get-secret-value \
  --secret-id <database_migration_user_secret_arn-or-name> \
  --query SecretString \
  --output text
```

- ecspresso render確認:

```sh
ecspresso --envfile .env --config ecspresso/migration/ecspresso.yml render task-definition
```

- IAM確認:

```sh
aws iam list-role-policies --role-name <task-execution-role-name>
aws iam get-role-policy --role-name <task-execution-role-name> --policy-name <policy-name>
```

- migration verify:

```sh
MIGRATION_COMMAND=verify ecspresso --config ecspresso/migration/ecspresso.yml run --wait
```

- エッジケース:
  - DB user は存在するが password が secret と異なる場合、`verify` が password authentication error で失敗することを確認し、`ALTER USER` または secret 更新で一致させる。
  - secret ARN が正しいが task execution role に権限がない場合、ECS task 起動時に secret fetch の `AccessDeniedException` が出ることを確認する。
  - `migrationuser` が schema/table/sequence 権限不足の場合、migration log に `permission denied` が出ることを確認する。
  - render 結果に `database_app_user_secret_arn` が混入していないことを文字列検索で確認する。

## 7. Risks & Mitigations
- Risk: `CREATE USER migrationuser` は既存ユーザーがあると失敗する。Mitigation: 必ず先に `SELECT usename FROM pg_user WHERE usename = 'migrationuser';` を実行し、0行の場合だけ作成する。
- Risk: `migrationpassword` がデフォルトから変更済みの場合、DBとSecrets Managerの片方だけ更新すると認証失敗する。Mitigation: Secrets Manager の `password` を信頼する値として扱い、DB側は `ALTER USER` で同じ値へ揃えるか、Terraform変数側も同じ値に更新する。
- Risk: 手動で Secrets Manager を編集しても、Terraform 管理値とズレると次回 apply で戻る可能性がある。Mitigation: `/Users/horikawa/GolandProjects/infra-skill-trail/modules/database/variables.tf:46-56` の変数と環境の tfvars/secret 注入方針を確認し、恒久対応は Terraform 入力に反映する。
- Risk: IAM policy のコードは正しくても、対象環境に未applyだと ECS task は secret を読めない。Mitigation: `/Users/horikawa/GolandProjects/infra-skill-trail/modules/containers/iam.tf:56-76` だけでなく、AWS上の実role policyを `aws iam get-role-policy` で確認する。
- Risk: 既存テーブル/シーケンスへの権限は付与されても、将来作成されるオブジェクトへの default privileges は別途必要になる可能性がある。Mitigation: migrationuser 自身が migration を作成する運用なら影響は小さいが、別ownerが作成する場合は `ALTER DEFAULT PRIVILEGES` の追加を別途検討する。