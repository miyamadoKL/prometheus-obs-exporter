# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

## [Unreleased]

全面改修：本プロジェクトは `paychex/prometheus-emcecs-exporter` から fork し、Dell ObjectScale 4.1.x と Dell EMC ECS 3.8.1.x を 1 バイナリで対応するよう作り直したものです。
設計の詳細は `docs/design.md` を参照してください。

### Breaking Changes
- モジュール名を `github.com/paychex/prometheus-emcecs-exporter` から `github.com/miyamadoKL/prometheus-obs-exporter` へ変更し、バイナリ名も `obs-exporter` に変更しました。
- メトリクス名の prefix を `emcecs_` から `obs_` に変更しました。メトリクス名、型、ラベル構成は全 collector にわたって見直しています（対応表は README を参照）。旧メトリクス名に依存する dashboard/alert は更新が必要です。
- スクレイプ用エンドポイントを `/query` から `/probe` に変更しました。
- フラグと環境変数の体系を刷新しました。フラグは `--ecs.*` / `--collector.*` / `--web.*` の namespace を使用し、環境変数は `ECSENV_*` の代わりに `OBS_EXPORTER_*` prefix を使用します。
- `metering` collector はデフォルトでは有効化されなくなりました。`/probe` で `collectors=metering` を明示指定する必要があります（従来は `metering=1` の真偽値フラグでデフォルト collector と併せて有効化していました）。
- ECS/ObjectScale に対する TLS 検証がデフォルトで有効になりました。従来は常にスキップされていました。自己署名証明書の信頼を復元するには `--ecs.tls.insecure-skip-verify` または `--ecs.tls.ca-file` を使用してください。
- CI を Travis CI から GitHub Actions へ移行し、`.goreleaser.yml` を GoReleaser v2 向けに書き直しました。

### Added
- 同一バイナリで ECS 3.8.1.x に加えて ObjectScale 4.1.x に対応しました。
- `--collector.perf.range` は起動時に検証され、Flux API で規定された range の上限（1h）を超える場合は拒否されるようになりました。
- dashboard API から ECS 3.6.0.0 で削除されたトランザクション系メトリクスを置き換える、Flux API ベースの `perf` collector を追加しました。
- `--ecs.mgmt-port` が metering API にも適用されるようになりました（従来はハードコードされていました）。
- `:9101` が利用できない環境向けに、DT 統計/ping collector を無効化する `--collector.node.dt-stats` を追加しました。
- 自己署名証明書向けに特定の CA を信頼する `--ecs.tls.ca-file` を追加しました。
- exporter 自身の listener に TLS/Basic 認証を設定する `--web.config.file`（`prometheus/exporter-toolkit` 経由）に対応しました。
- push、pull request ごとに build、test、`golangci-lint` を実行する GitHub Actions CI（`.github/workflows/ci.yml`）を追加しました。
- `v*` タグの push で GoReleaser v2 を実行する GitHub Actions リリースワークフロー（`.github/workflows/release.yml`）を追加しました。
- 標準的な linter セット（govet, staticcheck, errcheck, ineffassign, unused, gofmt, goimports）を定義した `.golangci.yml` を追加しました。
- distroless かつ非 root で動作する `obs-exporter` コンテナイメージをビルドする `Dockerfile` を追加しました。
- `docker compose` で exporter を起動する `compose.yaml` を追加しました。README には認証情報、TLS、トラブルシューティングを扱う日本語の「接続設定」セクションも追加しています。

### Removed
- `.travis.yml` と Travis CI 連携を削除しました。
- gjson による動的な JSON パース、logrus によるロギング、envy による環境変数処理を削除し、型付き構造体、`log/slog`（`prometheus/common/promslog`）、`kingpin` 標準の `Envar` サポートに置き換えました。

### Changed
- node collector のメトリクス名を変更しました：`obs_node_dt_total` / `obs_node_dt_unready` / `obs_node_dt_unknown` → `obs_node_directory_tables` / `obs_node_directory_tables_unready` / `obs_node_directory_tables_unknown`（DT = directory table。`_total` という名前の Gauge は Prometheus の命名規約に違反していたため改名しました）。
- `obs_metering_namespace_quota_bytes` は quota API の `blockSize` が 0 より大きい場合にのみ出力されるようになりました（ECS で設定可能な最小クォータは 1GB のため、`0` は実際の 0 バイトクォータではなく「未設定」を意味します）。
- collector の設定（`--collector.node.dt-stats`, `--collector.metering.concurrency`, `--collector.perf.range`）は package レベルの変数ではなく、リクエストスコープの `collector.Settings` 値として受け渡すようになり、`cfg` からすべての `/probe` 呼び出しへ完全に配線されています。
- シャットダウン時、exporter はキャッシュ済みターゲットからの logout を行う前に、実行中の `/probe` リクエストを `srv.Shutdown` で drain するようになりました（従来は順序が逆でした）。

## [1.0.0] - 2018-05-17
初回リリース - [Mark DeNeve](https://github.com/xphyr)

## [1.1.0] - 2018-09-24
発生していたログイン/ログアウトを削減するため、認証システムを変更しました - [Mark DeNeve](https://github.com/xphyr)

## [1.2.0] - 2019-07-13
プロジェクトのレイアウトを更新し、メモリ使用量を削減するため http クライアントの利用方法を改善しました。
また、デフォルトで go modules を使用するように変更し、vendor 化された依存関係をすべて削除しました。
ノード情報は SSL を有効にするためポート 9021 経由で取得するようになりました。
ECS アレイがファイアウォールの内側にある場合は、ポート 9020 の代わりにポート 9021 を許可するようルールを更新してください。
ロギングは Logrus のみを使用するように更新し、時刻フォーマットは人間に読みやすい形式に更新しました。
[Mark DeNeve](https://github.com/xphyr)
