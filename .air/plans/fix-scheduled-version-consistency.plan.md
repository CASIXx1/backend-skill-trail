## 1. Goal
scheduled-log / scheduled-notification の ECS task definition 登録時に、`versionConsistency: ""` が残って AWS ECS `RegisterTaskDefinition` で失敗しないようにする。

## 2. Approach
`ecspresso/scheduled-log/task-def.json:11` / `ecspresso/scheduled-log/task-def.json:43` と `ecspresso/scheduled-notification/task-def.json:11` / `ecspresso/scheduled-notification/task-def.json:43` には既に `"versionConsistency": "enabled"` が入っているため、この指定は維持する。実際の失敗箇所は `.github/workflows/deploy-backend.yml:137-140` の register 前 render 出力なので、`jq 'walk(if type == "object" and .versionConsistency == "" then del(.versionConsistency) else . end)'` を register 用 JSON 作成に挟む。README のローカル手順 `README.md:105-108` も同じ形に更新し、CI と手元実行の差分をなくす。

## 3. File Changes
- **Modify** `ecspresso/scheduled-log/task-def.json`
  - `containerDefinitions[0]` の `versionConsistency` は `ecspresso/scheduled-log/task-def.json:11` に存在するため維持する。
  - `containerDefinitions[1]` の `log_router` 用 `versionConsistency` は `ecspresso/scheduled-log/task-def.json:43` に存在するため維持する。
  - 実装時に欠落していないことを確認し、もしブランチ差分で消えていれば同じ位置へ `"versionConsistency": "enabled"` を追加する。

- **Modify** `ecspresso/scheduled-notification/task-def.json`
  - `containerDefinitions[0]` の `versionConsistency` は `ecspresso/scheduled-notification/task-def.json:11` に存在するため維持する。
  - `containerDefinitions[1]` の `log_router` 用 `versionConsistency` は `ecspresso/scheduled-notification/task-def.json:43` に存在するため維持する。
  - 実装時に欠落していないことを確認し、もしブランチ差分で消えていれば同じ位置へ `"versionConsistency": "enabled"` を追加する。

- **Modify** `.github/workflows/deploy-backend.yml`
  - `.github/workflows/deploy-backend.yml:137` の scheduled-log render を、標準出力から直接 `/tmp/scheduled-log-task-def.json` へ書く形ではなく、`jq 'walk(...)' > /tmp/scheduled-log-task-def.json` にパイプする形へ変更する。
  - `.github/workflows/deploy-backend.yml:139` の scheduled-notification render も同様に、`jq 'walk(...)' > /tmp/scheduled-notification-task-def.json` を挟む。
  - `.github/workflows/deploy-backend.yml:138` と `.github/workflows/deploy-backend.yml:140` の `aws ecs register-task-definition --cli-input-json file:///tmp/...` はそのまま維持する。

- **Modify** `README.md`
  - `README.md:105` の scheduled-log ローカル register 手順を、CI と同じ `ecspresso ... render task-definition | jq 'walk(...)' > /tmp/scheduled-log-task-def.json` に変更する。
  - `README.md:107` の scheduled-notification ローカル register 手順も同じ `jq` フィルタ付きに変更する。
  - `README.md:106` と `README.md:108` の `aws ecs register-task-definition` はそのまま維持する。

## 4. Implementation Steps
### Task 1: Confirm task definition inputs
1. `ecspresso/scheduled-log/task-def.json:7-43` を確認し、scheduled-log container と `log_router` container の両方に `"versionConsistency": "enabled"` がある状態を維持する。
2. `ecspresso/scheduled-notification/task-def.json:7-43` を確認し、scheduled-notification 用 container と `log_router` container の両方に `"versionConsistency": "enabled"` がある状態を維持する。

### Task 2: Normalize CI render output before register
1. `.github/workflows/deploy-backend.yml:137` を以下の形へ変更する。
   ```sh
   ecspresso --config ecspresso/scheduled-log/ecspresso.yml render task-definition \
     | jq 'walk(if type == "object" and .versionConsistency == "" then del(.versionConsistency) else . end)' \
     > /tmp/scheduled-log-task-def.json
   ```
2. `.github/workflows/deploy-backend.yml:138` の scheduled-log register は、生成済み `/tmp/scheduled-log-task-def.json` を使うまま維持する。
3. `.github/workflows/deploy-backend.yml:139` を以下の形へ変更する。
   ```sh
   ecspresso --config ecspresso/scheduled-notification/ecspresso.yml render task-definition \
     | jq 'walk(if type == "object" and .versionConsistency == "" then del(.versionConsistency) else . end)' \
     > /tmp/scheduled-notification-task-def.json
   ```
4. `.github/workflows/deploy-backend.yml:140` の scheduled-notification register は、生成済み `/tmp/scheduled-notification-task-def.json` を使うまま維持する。

### Task 3: Keep local documentation aligned
1. `README.md:105-108` の deploy 手順を `.github/workflows/deploy-backend.yml:137-140` と同じ `jq walk(...)` 付きの形へ更新する。
2. README のコマンド例では既存どおり `--envfile .env` を維持し、CI 側では既存どおり `--envfile` なしを維持する。

## 5. Acceptance Criteria
- `ecspresso/scheduled-log/task-def.json` の `containerDefinitions` 2 件に `"versionConsistency": "enabled"` が存在する。
- `ecspresso/scheduled-notification/task-def.json` の `containerDefinitions` 2 件に `"versionConsistency": "enabled"` が存在する。
- `.github/workflows/deploy-backend.yml` の scheduled-log register 用 JSON 作成で、`ecspresso --config ecspresso/scheduled-log/ecspresso.yml render task-definition` の出力が `jq 'walk(if type == "object" and .versionConsistency == "" then del(.versionConsistency) else . end)'` を通る。
- `.github/workflows/deploy-backend.yml` の scheduled-notification register 用 JSON 作成で、`ecspresso --config ecspresso/scheduled-notification/ecspresso.yml render task-definition` の出力が同じ `jq walk(...)` を通る。
- `.github/workflows/deploy-backend.yml` で `aws ecs register-task-definition` が参照する `/tmp/scheduled-log-task-def.json` と `/tmp/scheduled-notification-task-def.json` は、`jq` 適用後のファイルである。
- `README.md:105-108` のローカル deploy 手順が、scheduled-log / scheduled-notification の両方で `jq walk(...)` 適用後に `/tmp/...-task-def.json` を作成する例になっている。
- `jq` フィルタは `versionConsistency == ""` のプロパティだけを削除し、`versionConsistency == "enabled"` は削除しない。

## 6. Verification Steps
- YAML の該当行を確認する。
  ```sh
  sed -n '132,142p' .github/workflows/deploy-backend.yml
  ```
- task definition の指定数を確認する。
  ```sh
  jq '[.containerDefinitions[].versionConsistency] | length == 2 and all(. == "enabled")' ecspresso/scheduled-log/task-def.json
  jq '[.containerDefinitions[].versionConsistency] | length == 2 and all(. == "enabled")' ecspresso/scheduled-notification/task-def.json
  ```
- `jq` フィルタが空文字だけを削除し、`enabled` を残すことを確認する。
  ```sh
  printf '%s\n' '{"containerDefinitions":[{"versionConsistency":""},{"versionConsistency":"enabled"},{"name":"x"}]}' \
    | jq 'walk(if type == "object" and .versionConsistency == "" then del(.versionConsistency) else . end)'
  ```
  期待結果: 1 件目の `versionConsistency` は削除され、2 件目の `"enabled"` は残る。
- GitHub Actions 上では、`Deploy with ecspresso` ステップの `aws ecs register-task-definition` が `unsupported version consistency ''` で失敗しないことを確認する。
- 手元環境で AWS / tfstate / env が揃っている場合のみ、README の更新後コマンドで `/tmp/scheduled-log-task-def.json` と `/tmp/scheduled-notification-task-def.json` を生成し、以下を確認する。
  ```sh
  jq '.. | objects | select(.versionConsistency? == "")' /tmp/scheduled-log-task-def.json
  jq '.. | objects | select(.versionConsistency? == "")' /tmp/scheduled-notification-task-def.json
  ```
  期待結果: どちらも出力なし。

## 7. Risks & Mitigations
- Risk: ecspresso render の検証ステップ `.github/workflows/deploy-backend.yml:114-117` は `/dev/null` に捨てており、register 用 JSON の正規化結果までは検証しない。
  - Mitigation: 実際に `aws ecs register-task-definition` へ渡す `.github/workflows/deploy-backend.yml:137-140` の JSON 生成箇所へ `jq` を入れるため、register の入力は確実に正規化される。
- Risk: `jq walk` は jq 1.6 以降前提で、GitHub-hosted `ubuntu-latest` には通常入っているが、セルフホスト runner で古い jq だと失敗する可能性がある。
  - Mitigation: 現行 workflow は `runs-on: ubuntu-latest` のためそのまま使う。セルフホストへ変える場合は jq 1.6+ を runner 前提条件に追加する。
- Risk: README のコマンド例と CI のコマンドで `--envfile .env` の有無が違う。
  - Mitigation: 既存の用途差を維持し、README はローカル実行用として `--envfile .env` を残し、CI は `GITHUB_ENV` 経由の既存環境変数を使う。