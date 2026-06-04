# Agent Instructions

このリポジトリは skill-trail の backend service です。作業は段階的に進め、ユーザーが依頼した現在の段階に変更範囲を絞る。

## Project Context

- application code は `cmd/server` にある。
- GitHub Actions workflow は `.github/workflows` にある。
- ECS deploy 定義は `ecspresso` にある。
- local image push helper は `scripts/push-image.sh` にある。
- Terraform infrastructure はこのリポジトリ外で管理されている。
- ID、ARN、bucket 名、state key、region などの具体的な環境値は git 管理しているファイルに書かない。README や workflow には placeholder だけを書く。

## Working Rules

- 明示的に依頼されていない限り、AWS deploy 処理を追加しない。
- CI、AWS 接続確認、image push、ECS deploy は別々の段階として扱う。
- secret、ID、ARN、AWS account ID、token、環境変数の値をログに出さない。
- GitHub Actions を編集するときは、`.agent/SKILLS.md` の `GitHub Actions` skill に従う。
- workflow permissions は最小権限にする。
- AWS workflow の具体値は GitHub Environment `dev` の secrets に置く。GitHub variables は自動 mask されないため使わない。
- `environment: dev` を AWS OIDC と組み合わせる場合、Terraform 側は `repo:CASIXx1/backend-skill-trail:environment:dev` という `sub` claim を許可する必要がある。
- Go code を変更した後は `go test ./...` と `go vet ./...` を実行する。
- local sandbox の制約で Go cache に書けない場合は、repository-local な一時 `GOCACHE` を使い、検証後に削除する。
- workflow files を編集した後は YAML syntax を検証する。
