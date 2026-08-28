# Aster DNS

[![CI](https://github.com/starhui-dev/aster-dns/actions/workflows/ci.yml/badge.svg)](https://github.com/starhui-dev/aster-dns/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[中文](README.md) | [English](README.en.md) | [日本語](README.ja.md)

Aster DNS は、Huawei Cloud DNS、Alibaba Cloud DNS、Tencent Cloud DNSPod、Cloudflare DNS に対応するセルフホスト型・同一オリジンの DNS 管理コントロールプレーンです。Zone と Record の真の情報源は Provider API です。PostgreSQL には、プラットフォームの状態、Zone インデックス、短期間のキャッシュメタデータ、暗号化された認証情報、セッション、監査イベントだけを保存し、desired state の Record データベースとしては使用しません。

4 つの公式 Provider アダプター、統合 DNS サービス/API、capability-driven な SolidJS コンソール、設定可能な Argon2id パスワードログインを備えた Passkey 優先認証、TOTP、opaque session、RBAC、CSRF/Origin 保護、不変監査イベント、プロダクション向けの強化機能を実装しています。Unit、fixture、conformance のテストカバレッジは、実際の Provider アカウントに対する mutation が実行されたことを意味しません。ゲート付き統合テストの証跡は [`docs/TEST_MATRIX.md`](docs/TEST_MATRIX.md) を参照してください。

## 技術スタック

- Go 1.27、`net/http`、chi、pgx、埋め込み SQL migration
- ローカルおよび CI 検証用の PostgreSQL 18
- SolidJS、TypeScript strict、Vite、Tailwind CSS、Vitest、ESLint、Prettier
- ビルド済み SPA と `/api/v1` を同一オリジンから配信する単一アプリケーションコンテナ
- 公式 Provider SDK/API クライアントのみを使用し、実行時に DNS 集約・オーケストレーション層へ依存しない構成

ランタイムのバージョンは `mise.toml` に固定されています。フロントエンドのコミット済み lockfile は `web/package-lock.json` です。フロントエンドのコマンドには npm を使用してください。

## 前提条件

- Go 1.27
- Node.js 24 と npm 12
- Compose 対応の Docker Engine
- 乱数値を生成するための `openssl`

`mise` がインストール済みの場合は、`mise install` を実行して固定バージョンをインストールしてください。

## プロダクションデプロイ

### 1. Secret と設定を準備する

プレースホルダーファイルをコピーし、すべてのプレースホルダーを置き換えてください。`.env` はコミットしないでください。

```sh
cp .env.example .env
chmod 600 .env
```

プロダクションでは次の値を設定する必要があります。

```text
APP_ENV=production
APP_PUBLIC_URL=https://dns.example.com
APP_COMPOSE_PUBLIC_URL=https://dns.example.com
APP_DATABASE_URL=postgres://<db-user>:<db-password>@postgres:5432/<db-name>?sslmode=require
APP_MASTER_KEY=<32 バイトのランダム値を base64 エンコードしたもの>
APP_MASTER_KEY_VERSION=1
APP_BOOTSTRAP_TOKEN=<一度だけ使用する 32 バイトの base64url token>
```

`APP_PUBLIC_URL` と `APP_COMPOSE_PUBLIC_URL` は、ブラウザーから見える同一オリジンの HTTPS URL でなければなりません。Compose ファイルは `APP_COMPOSE_PUBLIC_URL` をコンテナ内の `APP_PUBLIC_URL` にマッピングします。内部ホスト名や、ユーザーが開く URL と異なるポートを指定してはいけません。
ルートの Compose 例では、PostgreSQL の証明書を用意しないローカル PostgreSQL サービスのため、`POSTGRES_SSLMODE=disable` がデフォルトです。`POSTGRES_SSLMODE=require` は PostgreSQL TLS を設定した後にだけ指定してください。上記のプロダクション基準は、TLS 対応のデータベースエンドポイントを前提としています。

PostgreSQL のパスワードは必須です。Compose にはデフォルトのデータベースパスワードも管理者パスワードもありません。Provider の認証情報は bootstrap 後に管理者 UI/API から入力し、通常の環境変数として設定しないでください。

### 2. Master key と bootstrap token を生成する

認証付き暗号化に使用する 32 バイトの master key を生成します。

```sh
umask 077
mkdir -p secrets
openssl rand -base64 32 > secrets/master-key.b64
chmod 600 secrets/master-key.b64
```

キーを `APP_MASTER_KEY` として注入するか、デプロイ用の secret manager を通じてファイルをマウントし、`APP_MASTER_KEY_FILE` を設定してください。2 つの設定は必ずどちらか一方だけにします。キー/keyring は PostgreSQL とは独立して、別のアクセス制御された障害ドメインにバックアップしてください。対応する master key がなければ、PostgreSQL のバックアップだけでは Provider の認証情報や TOTP secret を復元できません。master key を PostgreSQL、Git、イメージ、ログ、監査データに保存しないでください。

最初の管理者用の一度だけ使う token を生成します。

```sh
openssl rand 32 | base64 -w0 | tr '+/' '-_' | tr -d '='
```

最初の管理者が bootstrap を完了するまでだけ `APP_BOOTSTRAP_TOKEN` を保持してください。管理者はパスワードまたは Passkey を選択できます。成功後、直ちに token を環境から削除してアプリを再起動します。サーバーは弱いフォールバックを生成せず、デフォルトの管理者パスワードも作成しません。

### 3. Compose スタックを起動する

ルートの `compose.yaml` は、PostgreSQL、1 回だけ実行する migration サービス、non-root のアプリケーションサービスを提供します。PostgreSQL は named volume のサービスです。任意のホストポートはローカル利用のため loopback にバインドされており、プロダクションの必須要件ではありません。

```sh
docker compose config --quiet
docker compose up --build -d
```

migration サービスは PostgreSQL が healthy になった後にだけ `server migrate up` を実行します。アプリケーションは migration が正常に完了するまで待機します。PostgreSQL へ直接アクセスする必要がないホストでは、デプロイ前に `postgres` の `ports` マッピングを削除してください。

既存データベースまたはアップグレードの場合は、新しい `serve` プロセスを起動する前に migration を明示的に実行します。

```sh
docker compose run --rm migrate
docker compose up -d app
```

`serve` は schema を暗黙に作成、再構築、アップグレードすることはありません。運用上 migration は forward-only です。破壊的な down migration ではなく、互換性のあるバックアップリストアまたは新しい forward migration を使用してください。

### 4. 一度だけの管理者 bootstrap を完了する

`APP_PUBLIC_URL` を開きます。bootstrap ページには最初の管理者が必要であることが表示され、一度だけの token を受け付け、パスワードまたは WebAuthn/Passkey を選択できます。パスワード方式では最初の管理者、パスワードハッシュ、session、監査イベントをアトミックにコミットし、Passkey 方式では Passkey と challenge の消費もコミットします。ユーザーが存在すると bootstrap は利用できません。成功後に `APP_BOOTSTRAP_TOKEN` を削除してください。

以後のユーザーは管理者が作成し、一度だけ使用するハッシュ化済み enrollment token で登録します。ロールは `admin`、`operator`、`viewer` です。

### 5. リバースプロキシと WebAuthn Origin

リバースプロキシで TLS を終端するか、HTTPS を直接提供してください。公開 `Host` header を保持し、次を設定します。

```text
APP_PUBLIC_URL=https://dns.example.com
APP_TRUSTED_PROXY_CIDRS=<実際のプロキシ CIDR（カンマ区切り）>
```

`APP_TRUSTED_PROXY_CIDRS` には、アプリへ直接接続できるネットワークだけを含めてください。`0.0.0.0/0` は使用しないでください。転送されたクライアント IP header は、直近の peer が信頼されている場合にだけ使用されます。`APP_PUBLIC_URL` は Secure cookie、WebAuthn RP ID/allowed origin、mutation Origin チェックの canonical origin です。任意の転送 origin は受け付けません。プロキシからアプリへの経路が HTTP であっても、プロダクション設定では HTTPS が必要です。

### 6. Health、readiness、shutdown

```sh
curl --fail https://dns.example.com/healthz
curl --fail https://dns.example.com/readyz
```

- `/healthz` はプロセスの生存状態だけを報告します。
- `/readyz` は PostgreSQL、埋め込み migration の正確なバージョン、永続化されたすべての暗号化 key version の利用可能性を確認します。
- イメージの healthcheck は `/app/server healthcheck --url http://127.0.0.1:8080/healthz` を呼び出します。
- SIGTERM/SIGINT は新しい HTTP 処理を停止し、`APP_SHUTDOWN_TIMEOUT` 内に処理中のリクエストを drain し、プロセス内 scheduler をキャンセルし、worker の終了を待って PostgreSQL pool を閉じます。

readiness の失敗を、アプリを healthy として公開することで回避してはいけません。master key の欠落や dirty/outdated な schema がある場合、サービスは意図的に未 ready のままになるか、起動を拒否します。

## フロントエンドのプロダクションアセット

Dockerfile はフロントエンド、Go、最小ランタイムの各ステージに分かれています。

1. `web-build` は `npm ci --ignore-scripts` と `npm run build` を実行します。
2. `go-build` は `CGO_ENABLED=0`、`-trimpath`、シンボル削除を指定して静的 Linux バイナリをコンパイルします。
3. `runtime` は `gcr.io/distroless/static-debian12:nonroot` で、`/app/server` と生成済みの `/app/web` アセットだけを含みます。

Go server は `/app/web` から Vite の出力を配信し、同一オリジンの SPA fallback を提供します。`/assets/` 内の hashed file には immutable cache を適用し、SPA のエントリーポイントは再検証します。これにより 2 つ目の静的サイト用 origin が不要になり、CSRF/CORS と WebAuthn origin の処理が単純になります。build context はローカル環境ファイル、secret ディレクトリ、key ファイル、フロントエンドの依存関係/ビルド出力を無視します。build secret が runtime image にコピーされることはありません。

## 運用とリカバリ

次の内容は [`docs/OPERATIONS.md`](docs/OPERATIONS.md) を参照してください。

- Provider 認証失敗と最小 IAM ガイダンス
- Provider `429` の処理と mutation の retry 禁止ルール
- Zone sync の失敗と authoritative refresh の動作
- master-key/key-version エラー
- 認証情報の置換とキャッシュ無効化
- PostgreSQL のバックアップ/リストア（独立した master-key 要件を含む）
- proxy、shutdown、scheduler、ログ、security header の運用

Scheduler は意図的に単一レプリカです。プロセス内で動作し、重複排除も 1 プロセス内に限られます。データベースベースの実際の leader/lease 機構を実装してテストするまでは、`app` を水平スケールしないでください。

ログは request ID、安全な actor/provider/Zone 識別子、operation、duration、安定したエラーコードを含む構造化 JSON です。Authorization、cookie、password、token、access key、signature、データベースパスワード、SDK error、panic stack、audit payload を redact します。現在のリリースは `/metrics` を公開せず、大規模な metrics/telemetry 依存も追加していません。現在の収集には JSON ログと health endpoint を使用してください。

## ローカル開発

ネイティブ開発では、`APP_PUBLIC_URL=http://localhost:5173` を設定した開発用 `.env` を使用し、データベースを設定する場合は明示的なランダム値の `APP_MASTER_KEY` も設定してください。

```sh
make setup
make dev-db
make migrate
```

ターミナル 1：

```sh
make dev-backend
```

ターミナル 2：

```sh
make dev-frontend
```

<http://localhost:5173> を開きます。Vite は `/api`、`/healthz`、`/readyz` を `127.0.0.1:8080` の Go server にプロキシします。ネイティブの `serve` は schema migration を実行しません。

## 品質ゲート

ルートの `Makefile` と `web/package.json` の scripts がコマンドの正式な参照元です。

```sh
make backend-format-check
make backend-lint
make backend-test
make frontend-format-check
make frontend-lint
make frontend-typecheck
make frontend-test
make build
make container-build IMAGE=aster-dns:release-candidate
```

リリースの完全な証跡とデプロイ固有のチェック項目は [`docs/RELEASE_CHECKLIST.md`](docs/RELEASE_CHECKLIST.md) で管理しています。実際の Provider 統合テストには専用の認証情報とテスト Zone が必要です。mutation テストには `DNS_INTEGRATION_MUTATE=1` も必要で、作成した一時レコードを必ず削除してください。

## HTTP インターフェース

- `GET /healthz`：プロセスの生存状態。
- `GET /readyz`：PostgreSQL/schema/encryption の readiness。
- `GET /api/v1`：API/build メタデータ。
- `/api/v1/auth/*`：bootstrap、Passkey/password/TOTP ログイン、現在の session、Passkey 管理、設定、ログアウト、session の失効。
- `/api/v1/users/*`：管理者専用のユーザー作成、ロール/無効状態の変更、enrollment-token の発行。
- `/api/v1/provider-accounts/*`、`/api/v1/zones/*`：Provider account、Zone、Record の操作。
- 不明な API ルートは安定した `{ "error": { "code", "message", "request_id" } }` envelope を返します。

Cookie 認証の mutation には、一致する同一オリジンの `Origin`、読み取り可能な CSRF cookie、`X-CSRF-Token` が必要です。opaque session cookie は HttpOnly で、PostgreSQL には token hash だけを保存します。Security header には CSP、frame denial、`nosniff`、Referrer/Permissions と cross-origin policy、DNS-prefetch denial、HTTPS の公開 URL に対する HSTS が含まれます。

## ライセンス

[Apache License 2.0](LICENSE) の下でライセンスされています。
