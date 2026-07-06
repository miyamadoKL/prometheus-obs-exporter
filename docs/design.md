# prometheus-obs-exporter 設計書

Dell ObjectScale 4.1.0.x / ECS 3.8.1.x 対応 Prometheus exporter への全面改修の設計を定める。
paychex/prometheus-emcecs-exporter (ECS 3.3 世代) からの fork を土台とするが、メトリクス互換は維持しない。

## 決定事項（2026-07-03 確定）

1. ECS 3.8.1.x と ObjectScale 4.1.0.x を 1 バイナリで両対応する
2. メトリクス prefix は `emcecs_` から `obs_` へ刷新する（後方互換なし）
3. ECS 3.6.0.0 で dashboard API から削除されたトランザクション系メトリクスは Flux API で提供する
4. 実機検証を最終工程とする（dashboard API の残存フィールドと DT 統計はマニュアルでは確認不能）

## 根拠となる API 調査結果

- **認証**：`GET /login`（Basic 認証）→ `X-SDS-AUTH-TOKEN` ヘッダ。
  - ポート 4443。
  - トークン有効期間 8h、アイドル 2h、ユーザーあたり同時 100 個上限。
  - `GET /user/whoami` で検証する。
  - `GET /logout` で失効する。
  - ECS 3.8 / ObjectScale 4.2 で共通。
- **dashboard API**（`/dashboard/zones/localzone` ほか）：両製品で存続する。
  - ただし ECS 3.6.0.0 で `transaction*` / `diskRead(Write)BandwidthTotal` / `nodeCpuUtilization` 等のフィールドが削除済みである。
- **代替**：Flux API。
  - `POST https://<host>:4443/flux/api/external/v2/query`、`X-SDS-AUTH-TOKEN` 認証、Flux クエリ、range 最大 1h 制約。
  - 詳細は `docs/api-research/flux-api-reference.md`。
- **metering**：`/object/namespaces`、`/object/namespaces/namespace/{ns}/quota`、`/object/billing/namespace/{ns}/info`。
  - 集計遅延は最大 15 分（S3 fan-out は 2h15m）。
- **DT 統計**：`http://<node>:9101/stats/dt/DTInitStat`（XML、平文）と `https://<node>:9021/?ping` はマニュアル記載なし。
  - 存続不明のため設定で無効化可能にする。
- 両製品とも Prometheus ネイティブ連携はない。
  - exporter 方式を継続する。

## 全体アーキテクチャ

マルチターゲット exporter パターンを維持する。

- `GET /probe?target=<host>[&collectors=a,b,c]`：指定クラスタをスクレイプする（旧 `/query` は廃止）
  - `collectors` 省略時：`cluster,replication,node,perf`
  - `metering` は重い（namespace 数に比例）ため明示指定時のみ実行する（旧 `metering=1` 相当）
- `GET /metrics`：exporter 自身のメトリクス（`obs_exporter_build_info` 等）
- `GET /`：ランディングページ

ターゲットごとの認証済みクライアントは従来同様プロセス内にキャッシュし、SIGTERM/SIGINT で全ターゲットへ logout する（トークン 100 個上限対策）。

## モジュールとツールチェーン

| 項目 | 変更後 |
|---|---|
| module path | `github.com/miyamadoKL/prometheus-obs-exporter` |
| go directive | `go 1.25.0`（`toolchain go1.25.11`、ローカル go1.22 から自動ダウンロードするが、golangci-lint の対応言語バージョン上限が go1.25 のため 1.26 は見送り） |
| ログ | `log/slog`（`prometheus/common/promslog`）、logrus は廃止 |
| フラグ/環境変数 | `alecthomas/kingpin/v2` + `.Envar("OBS_EXPORTER_*")`、envy は廃止 |
| メトリクス | `prometheus/client_golang` 最新 |
| listen 側 TLS/認証 | `prometheus/exporter-toolkit/web`（`--web.config.file`） |
| JSON パース | 型付き構造体（`encoding/json`）、gjson は廃止 |
| CI | GitHub Actions（build / test / golangci-lint / goreleaser v2）、Travis CI は廃止 |

gjson の文字列パス参照をやめ、API レスポンスを型付き構造体（コントラクト層）として `internal/obsclient/types.go` に定義する。
コントラクトを固定し、collector 実装は再生成可能に保つ。

## パッケージ構成

```
cmd/obs-exporter/main.go     エントリポイント、/probe ハンドラ、シグナル処理
internal/config/             kingpin 定義
internal/obsclient/
  client.go                  HTTP クライアント（TLS 設定、共通 GET/POST）
  auth.go                    トークン管理（取得・検証・失効、401 時は 1 回だけ再ログイン）
  types.go                   API レスポンスのコントラクト型
  dashboard.go               dashboard API（localzone / replicationgroups / vdc/nodes）
  metering.go                namespaces / quota / billing
  flux.go                    Flux API クライアント（クエリ組み立てとレスポンスパース）
  dtstats.go                 DT 統計 / ping（XML、設定で無効化可能）
internal/collector/
  cluster.go replication.go node.go metering.go perf.go
```

## 設定項目

| フラグ | 環境変数 | 既定値 | 備考 |
|---|---|---|---|
| `--web.listen-address` | OBS_EXPORTER_WEB_LISTEN_ADDRESS | `:9438` | exporter-toolkit 標準 |
| `--web.config.file` | なし | なし | listen 側 TLS/BASIC 認証 |
| `--ecs.username` | OBS_EXPORTER_USERNAME | なし（必須） | System Monitor ロール推奨 |
| `--ecs.password` | OBS_EXPORTER_PASSWORD | なし（必須） | |
| `--ecs.mgmt-port` | OBS_EXPORTER_MGMT_PORT | `4443` | metering にも適用（旧版のハードコードバグを修正） |
| `--ecs.obj-port` | OBS_EXPORTER_OBJ_PORT | `9021` | ping 用 |
| `--ecs.tls.insecure-skip-verify` | OBS_EXPORTER_TLS_INSECURE_SKIP_VERIFY | `false` | 旧版は常時 true だったが、オプトインに変更 |
| `--ecs.tls.ca-file` | OBS_EXPORTER_TLS_CA_FILE | なし | 自己署名証明書向け |
| `--collector.node.dt-stats` | OBS_EXPORTER_COLLECTOR_NODE_DT_STATS | `true` | :9101 存続不明のため無効化可能 |
| `--collector.metering.concurrency` | OBS_EXPORTER_METERING_CONCURRENCY | `4` | |
| `--collector.perf.range` | OBS_EXPORTER_PERF_RANGE | `5m` | Flux range（最大 1h） |
| `--log.level` / `--log.format` | なし | `info` / `logfmt` | promslog 標準 |

## メトリクスコントラクト

命名は Prometheus 規約に従う。
基本単位（bytes / seconds）を用い、counter には `_total` を付与し、単位換算は collector 内で行う。
共通ラベルはない。
`target` は Prometheus の relabel で付与される前提とする。
旧版の `target_name` ラベルは廃止する。

### メタメトリクス（exporter 自身の状態）

| メトリクス | ラベル | 型 | 内容 |
|---|---|---|---|
| `obs_scrape_success` | collector | Gauge | collector 単位の成否 |
| `obs_scrape_duration_seconds` | collector | Gauge | collector 単位の所要時間 |
| `obs_cluster_info` | vdc, version, product | Gauge(=1) | version は `/vdc/nodes` 由来、product は `ecs` / `objectscale` |
| `obs_exporter_build_info` | version, revision, goversion | Gauge(=1) | `/metrics` 側 |

### cluster collector（`/dashboard/zones/localzone`）

| メトリクス | ラベル | 型 | 元フィールド |
|---|---|---|---|
| `obs_cluster_nodes` | vdc, state=`good\|bad` | Gauge | numGoodNodes / numBadNodes |
| `obs_cluster_disks` | vdc, state=`good\|bad` | Gauge | numGoodDisks / numBadDisks |
| `obs_cluster_capacity_total_bytes` | vdc | Gauge | diskSpaceTotalCurrent.0.Space（GB→bytes 換算） |
| `obs_cluster_capacity_free_bytes` | vdc | Gauge | diskSpaceFreeCurrent.0.Space（同上） |
| `obs_cluster_alerts_unacknowledged` | vdc, severity=`critical\|error\|info\|warning` | Gauge | alertsNumUnack*（旧版の Counter 誤用を修正） |

トランザクション系（レイテンシ/TPS/帯域/エラー）は 3.6.0.0 で削除済みのため perf collector（Flux）へ移管する。

### replication collector（`/dashboard/zones/localzone/replicationgroups`）

旧版の `node` ラベル誤用を `rg`（replication group 名）に修正する。

| メトリクス | ラベル | 型 | 元フィールド |
|---|---|---|---|
| `obs_replication_ingress_bytes_per_second` | rg | Gauge | replicationIngressTraffic |
| `obs_replication_egress_bytes_per_second` | rg | Gauge | replicationEgressTraffic |
| `obs_replication_pending_repo_bytes` | rg | Gauge | chunksRepoPendingReplicationTotalSize |
| `obs_replication_pending_journal_bytes` | rg | Gauge | chunksJournalPendingReplicationTotalSize |
| `obs_replication_pending_xor_bytes` | rg | Gauge | chunksPendingXorTotalSize |
| `obs_replication_rpo_timestamp_seconds` | rg | Gauge | replicationRpoTimestamp（epoch 秒へ換算） |

### node collector（DT 統計 / ping。`--collector.node.dt-stats` で無効化可能）

| メトリクス | ラベル | 型 | 元 |
|---|---|---|---|
| `obs_node_directory_tables` / `obs_node_directory_tables_unready` / `obs_node_directory_tables_unknown` | node | Gauge | `/stats/dt/DTInitStat`（XML、DT = directory table） |
| `obs_node_active_connections` | node | Gauge | `/?ping`（XML） |

directory tables 系メトリクスは Gauge のため `_total` を付けない。
`_total` は Prometheus の命名規約で counter に予約されているため改名した。

### metering collector（明示指定時のみ）

| メトリクス | ラベル | 型 | 元 |
|---|---|---|---|
| `obs_metering_namespace_quota_bytes` | namespace, quota=`block\|notification` | Gauge | quota API（GB→bytes 換算） |
| `obs_metering_namespace_used_bytes` | namespace | Gauge | billing info total_size（KB→bytes 換算） |
| `obs_metering_namespace_objects` | namespace | Gauge | billing info total_objects |

`obs_metering_namespace_quota_bytes` は `blockSize > 0` のときのみ出力する（block/notification 両方）。
ECS のネームスペースクォータは最小 1GB のため、`blockSize == 0` は「クォータ未設定」を表すサンチネル値であり、実際の 0 バイトクォータではない（根拠：dell-ecs/admin-guide/05-namespaces.md）。

### perf collector（Flux API）

measurement / field の正確な対応は `docs/api-research/flux-api-reference.md` を正とする。
提供メトリクス（VDC 集約値、`monitoring_vdc` の `cq_performance_*` 系を優先し、なければ `monitoring_main` のノード別値を集約）：

| メトリクス | ラベル | 型 | 内容 |
|---|---|---|---|
| `obs_perf_read_latency_seconds` | vdc | Gauge | 旧 transactionReadLatencyCurrent（ms→s 換算） |
| `obs_perf_write_latency_seconds` | vdc | Gauge | 旧 transactionWriteLatencyCurrent |
| `obs_perf_read_bytes_per_second` | vdc | Gauge | 旧 transactionReadBandwidth |
| `obs_perf_write_bytes_per_second` | vdc | Gauge | 旧 transactionWriteBandwidth |
| `obs_perf_read_transactions_per_second` | vdc | Gauge | 旧 transactionReadTransactionsPerSec |
| `obs_perf_write_transactions_per_second` | vdc | Gauge | 旧 transactionWriteTransactionsPerSec |
| `obs_perf_transaction_errors` | vdc, category, error_type | Gauge | 旧 transactionErrors.types（文字列分解パースは廃止し、タグから直接取得） |

Flux クエリは `range(start: -<perf.range>)` + `last()` で最新値のみ取得する（range 最大 1h 制約に適合）。

measurement 対応（詳細と確度は flux-api-reference.md 参照）：

- **帯域**：`monitoring_vdc` の `cq_performance_throughput`（field は `total_read_requests_size` / `total_write_requests_size`）
- **TPS**：`cq_performance_transaction_method`（`method` タグ = READ/WRITE）
- **レイテンシ**：`cq_performance_latency`。
  - ただし read/write を判別する `id` タグ値はマニュアル記載なし。
  - 実機検証で確定するまで暫定実装とし、クエリ定義は定数テーブルに集約して差し替え可能にする
- **エラー**：`cq_performance_error*`（field は `system_errors` / `user_errors`）

注意：range 最大 1h 制約、procstat の process_name 列挙は ECS 版マニュアルのみに記載。
ObjectScale 側は記載なしのため同一仕様と仮定し、実機で確認する。

## エラー処理と信頼性

- `CallAPI` の 401 リトライは最大 1 回（旧版の無制限再帰を廃止）
- collector 単位で失敗を隔離し、`obs_scrape_success{collector}` に反映する（1 API の失敗で全体を落とさない）
- ターゲットごとのトークンは有効期限（8h）とアイドル（2h）を考慮するが、事前の whoami 検証は行わない。
  - 実装は 401 応答を起点に再ログイン（最大 1 回）する方式である。
  - 二重ログインは compare-and-clear（`invalidateToken`）とロック（`authMu`）で防止する（internal/obsclient/auth.go）。
- HTTP タイムアウトを明示設定する

## テスト

- `httptest.Server` + fixture（マニュアル記載のレスポンス例から作成）で obsclient / 各 collector をユニットテストする
- `promtest`（client_golang の testutil）でメトリクス出力を検証する

## 実装フェーズ

1. **Phase A**：モジュール改名、ツールチェーン刷新、config、main（/probe スケルトン）、旧コード削除
2. **Phase B**：obsclient（auth / client / types / dashboard / metering / dtstats）+ テスト
3. **Phase C**：collector 5 種 + テスト
4. **Phase D**：Flux クライアント + perf collector + テスト
5. **Phase E**：CI（GitHub Actions）、goreleaser v2、README/CHANGELOG 刷新
6. **Phase F**：レビュー、ビルドとテストを通しで実行、実機疎通検証（ユーザー提供の環境）
