---
name: github-actions
description: GitHub Actions workflow を作成または変更するときに使う。特に AWS OIDC、GitHub Environment variables、secrets、ID、ARN、AWS account ID、token、環境変数を扱う workflow では、値をログに出さず必ず mask するために使う。
---

# GitHub Actions

`.github/workflows/*.yml` または `.github/workflows/*.yaml` を編集するときは、この skill を使う。

## 基本方針

- workflow の責務は狭く保つ。現在の段階で必要な job または step だけを追加する。
- 明示的に依頼されていない限り、deploy、image push、infra 変更は追加しない。
- CI、AWS 接続確認、image push、ECS deploy は別々の段階として扱う。
- すべての workflow で最小権限の `permissions` を使う。
- workflow を編集した後は YAML syntax を検証する。
- workflow に長い inline shell を書かない。mask、tfstate 読み込み、値の整形などの共通処理は `.github/scripts/*.sh` に切り出す。
- `.github/scripts/*.sh` に切り出した場合は、workflow YAML だけでなく `bash -n` で script syntax も検証する。

## ログ保護ルール

- ID、ARN、AWS account ID、token、secret、環境変数の値をログに出さない。
- ID と環境変数は、意図せず表示される可能性がある step より前に必ず mask する。
- mask 処理を script に切り出す場合も、値を `$GITHUB_ENV` に書き込む前に `::add-mask::` を実行する。
- Terraform state から取得した output、map の各値、CSV から展開した個別 ID は、それぞれ個別に mask する。
- 複数行の値を `$GITHUB_ENV` に直接書かない。JSON 配列などは `jq -c` などで 1 行に整形してから書く。
- GitHub Environment variables は自動 mask されない。ログに出したくない具体値は GitHub Environment secrets に置く。
- GitHub Environment secrets を使う場合も、参照するすべての値に `::add-mask::` を明示的に設定する。
- `aws-actions/configure-aws-credentials` を使う場合は、必ず `mask-aws-account-id: true` を設定する。
- `assumedRoleId` などの ID もログに出したくない場合は、`aws-actions/configure-aws-credentials` を使わず、GitHub OIDC token を取得して `aws sts assume-role-with-web-identity` を手動実行する。
- identity 確認コマンドの出力は `/dev/null` に捨て、固定の成功メッセージだけを出す。
- 必須 variable の検証は空判定だけにする。値は echo しない。

## AWS OIDC ルール

- AWS role を assume する workflow または job にだけ `id-token: write` を付与する。
- environment 単位の approval や variable が必要な場合は、`dev` などの GitHub Environment を優先する。
- workflow で `environment: dev` を使う場合、GitHub OIDC の `sub` claim は branch ref ではなく `repo:<owner>/<repo>:environment:dev` になる。
- `environment: dev` を使う場合、Terraform 側の IAM trust policy では `repo:CASIXx1/backend-skill-trail:environment:dev` を許可する必要がある。
