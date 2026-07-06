# prometheus-obs-exporter

[![Go Report Card](https://goreportcard.com/badge/github.com/miyamadoKL/prometheus-obs-exporter)](https://goreportcard.com/report/github.com/miyamadoKL/prometheus-obs-exporter)

**Dell ObjectScale 4.1.x** および **Dell EMC ECS 3.8.1.x** クラスタ向けの、Prometheus マルチターゲット exporter です。

本プロジェクトは [paychex/prometheus-emcecs-exporter](https://github.com/paychex/prometheus-emcecs-exporter)（ECS 3.3.x を対象としていました）のフォークで、ECS 3.8.1.x と Dell ObjectScale 4.1.x の現行 dashboard/Flux API に対応するよう単一バイナリとして書き直したものです。
このツールを ECS/ObjectScale ノード自体で動かすことは推奨しません。
クラスタの管理 API へ到達できる別のマシン上で動かしてください。

## prometheus-emcecs-exporter からの破壊的変更

本プロジェクトは全面的な書き直しです。
`paychex/prometheus-emcecs-exporter` から移行する場合、次の非互換に注意してください。

- **メトリクス名のプレフィックスが変更**：`emcecs_` → `obs_`。
  メトリクス名、ラベルの組み合わせ、メトリクスの型もすべて見直しました（詳細は後述の[メトリクス](#公開されるメトリクス一覧)を参照）。
  旧名で構築したダッシュボードやアラートは書き直しが必要です。
- **スクレイプエンドポイントが変更**：`/query` → `/probe`。
  `scrape_configs` の `metrics_path` を更新してください。
- **フラグと環境変数の体系が変更**：フラグは [kingpin](https://github.com/alecthomas/kingpin) によるドット区切りの `--ecs.*` / `--collector.*` / `--web.*` の名前空間に変わり、環境変数は `ECSENV_*` に代わって `OBS_EXPORTER_*` プレフィックスを使うようになりました。
  対応表の全体は後述の[設定](#起動時に指定できる設定項目)を参照してください。
- **`metering` collector は明示的に指定が必要**：`/probe` のクエリ文字列で `collectors=metering` を指定してください（他の collector と併記しても構いません）。
  従来のような真偽値 `metering=1` のトグルはなくなりました。
  metering はネームスペース数に比例して重くなり得るため、デフォルトの collector セットからも外れています。
- **TLS 検証はオプトイン**：旧 exporter は ECS との通信時に常に TLS 検証をスキップしていました。
  本 exporter はデフォルトで証明書を検証します。
  旧来の挙動に戻す、あるいは自己署名証明書を信頼するには `--ecs.tls.insecure-skip-verify` または `--ecs.tls.ca-file` を使ってください。
- **CI/リリースの仕組みを刷新**：Travis CI と旧 `goreleaser` v0 の設定は廃止し、GitHub Actions と `goreleaser` v2 に置き換えました（詳細は[開発](#開発者向けの手順)を参照）。

## 起動時に指定できる設定項目

すべての設定項目は、コマンドラインフラグまたは対応する環境変数のどちらでも指定できます。

| フラグ | 環境変数 | デフォルト値 | 備考 |
| --- | --- | --- | --- |
| `--web.listen-address` | `OBS_EXPORTER_WEB_LISTEN_ADDRESS` | `:9438` | 標準の `exporter-toolkit` web フラグ（複数指定可） |
| `--web.config.file` | なし | (なし) | exporter 自身のリスナーに対する TLS/Basic 認証の設定（[exporter-toolkit web config](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md)） |
| `--ecs.username` | `OBS_EXPORTER_USERNAME` | (なし、必須) | ECS/ObjectScale のユーザー名（**System Monitor** ロールを推奨） |
| `--ecs.password` | `OBS_EXPORTER_PASSWORD` | (なし、必須) | ECS/ObjectScale のパスワード |
| `--ecs.mgmt-port` | `OBS_EXPORTER_MGMT_PORT` | `4443` | 管理 API のポートで、metering API にも使う（旧 exporter にあったポート固定バグを修正） |
| `--ecs.obj-port` | `OBS_EXPORTER_OBJ_PORT` | `9021` | オブジェクト（S3）API のポートで、ノードの ping チェックに使う |
| `--ecs.tls.insecure-skip-verify` | `OBS_EXPORTER_TLS_INSECURE_SKIP_VERIFY` | `false` | ECS/ObjectScale に対する TLS 証明書検証をスキップするオプトイン設定（旧 exporter は常にスキップしていた） |
| `--ecs.tls.ca-file` | `OBS_EXPORTER_TLS_CA_FILE` | (なし) | 自己署名の ECS/ObjectScale 証明書を検証するための PEM 形式 CA バンドル |
| `--collector.node.dt-stats` | `OBS_EXPORTER_COLLECTOR_NODE_DT_STATS` | `true` | DT 統計 / ping ノード collector を有効化する設定（`:9101` が存在しないプラットフォームでは無効化する） |
| `--collector.metering.concurrency` | `OBS_EXPORTER_METERING_CONCURRENCY` | `4` | metering API に対して同時に発行するリクエストの最大数 |
| `--collector.perf.range` | `OBS_EXPORTER_PERF_RANGE` | `5m` | perf collector の Flux クエリ範囲（Flux API の制約により最大 `1h`） |
| `--log.level` | なし | `info` | 標準の `promslog` フラグ |
| `--log.format` | なし | `logfmt` | 標準の `promslog` フラグ |

フラグの正確な一覧は `obs-exporter --help` で確認してください。

## 必要な ECS / ObjectScale のロール

設定するユーザーには、クラスタのヘルス、レプリケーション、ノード、metering データへの読み取り権限があれば十分です。
ビルトインロールの「System Monitor」を推奨します。
このロールはプロビジョニングや管理操作の権限を一切持ちません。

## TLS 設定

exporter はデフォルトで、ECS/ObjectScale の管理 API が提示する TLS 証明書を検証します。
クラスタが自己署名証明書を使っている場合、デフォルトの検証のままでは失敗するため、次のいずれかを指定してください。

- `--ecs.tls.ca-file=/path/to/ca.pem`（環境変数 `OBS_EXPORTER_TLS_CA_FILE`）：特定の CA を信頼する（推奨）
- `--ecs.tls.insecure-skip-verify`（環境変数 `OBS_EXPORTER_TLS_INSECURE_SKIP_VERIFY=true`）：検証を完全に無効化する（検証用途に限り、本番環境で使うわけにはいかない）

exporter 自身の HTTP リスナーは、`--web.config.file` によって TLS や Basic 認証を別途設定できます。
詳細は[exporter-toolkit の web configuration ドキュメント](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md)を参照してください。

CA ファイルを使う場合、Docker Compose では `compose.yaml` の volumes 例のコメントを解除してマウントしてください（後述の[Docker Compose での起動](#docker-compose-での起動)を参照）。

## `/probe` エンドポイント

```
GET /probe?target=<host>[&collectors=cluster,replication,node,perf,metering]
```

- `target`（必須）：スクレイプ対象とする ECS/ObjectScale クラスタのホスト名または IP。
- `collectors`（任意、カンマ区切り）：このスクレイプで実行する collector。
  デフォルトは `cluster,replication,node,perf`。
  - `metering` はデフォルトに含まれません。
    クラスタ上のネームスペース数に比例して重くなり、時間がかかることがあるためです。
    使う場合は明示的に指定し、できれば独立した低頻度のスクレイプジョブとして実行してください。

その他のエンドポイント。

- `GET /metrics`：exporter 自身のプロセス/ビルドに関するメトリクス（`obs_exporter_build_info`、Go ランタイムの統計など）
- `GET /`：上記エンドポイントへのリンクを掲載したランディングページ

認証済みクライアントは、プロセスが動いている間ターゲットごとにキャッシュされます。
`SIGTERM`/`SIGINT` を受けると、exporter はキャッシュしているすべてのターゲットからログアウトします（ECS/ObjectScale はユーザーあたりの有効トークン数を 100 個に制限しています）。

## Prometheus の設定

このマルチターゲット exporter パターン（[SNMP exporter](https://github.com/prometheus/snmp_exporter)や[blackbox exporter](https://github.com/prometheus/blackbox_exporter)と同じ方式）では、1 つの exporter プロセスが複数の ECS/ObjectScale クラスタをスクレイプできます。
ここでいう「target」は監視対象の ECS/ObjectScale クラスタのホスト名を指し、「exporter」はそれとは別のマシン上で常時動いているプロセスそのものを指します。
両者は別物である点に注意してください。
Prometheus が直接スクレイプするのは常に exporter（下記の例では `127.0.0.1:9438`）であり、exporter が受け取った `target` パラメータをもとに、自分の代わりに ECS/ObjectScale クラスタへリクエストを飛ばして結果を返します。

そのため `relabel_configs` による書き換えが必要になります。
`static_configs` の `targets` に列挙したクラスタのホスト名は、そのままでは Prometheus 自身がスクレイプ先アドレスとして使おうとしてしまいます。
そこで次の3段階の relabel を行います。

- `__address__`（`targets` に書いたクラスタのホスト名）を `__param_target` にコピーし、`/probe` の `target` クエリパラメータとして使えるようにします。
- `__param_target` の値を `instance` ラベルにコピーし、どのクラスタのメトリクスかをラベルで区別できるようにします。
- `__address__` を exporter 自身が listen しているアドレス（`127.0.0.1:9438` など）に置き換え、実際のスクレイプ先を exporter に付け替えます。

`metrics_path` を `/probe` にしているのは、この exporter の主要な機能がこのエンドポイント経由で提供されるためです（前述の「[`/probe` エンドポイント](#probe-エンドポイント)」を参照）。

```yaml
scrape_configs:
  # Cluster health, replication, node, and perf metrics.
  - job_name: "obs-exporter"
    metrics_path: /probe
    static_configs:
      - targets:
          - myecsarray-1.example.net # ECS/ObjectScale cluster/VDC
          - myecsarray-2.example.net
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: 127.0.0.1:9438 # host:port the exporter itself listens on

  # metering (namespace の quota/usage) メトリクスはネームスペース数に比例して重く、
  # 変化も遅いため、`collectors=metering` を指定した独立の低頻度ジョブにする。
  - job_name: "obs-exporter-metering"
    scrape_interval: 300s
    scrape_timeout: 60s
    metrics_path: /probe
    params:
      collectors: ["metering"]
    static_configs:
      - targets:
          - myecsarray-1.example.net
          - myecsarray-2.example.net
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: 127.0.0.1:9438

  # exporter 自身のプロセス/ビルドに関するメトリクス。
  - job_name: "obs-exporter-self"
    static_configs:
      - targets:
          - 127.0.0.1:9438
```

大規模なクラスタでは、デフォルトより長い `scrape_timeout` が必要になる場合があります。

exporter は監視対象のクラスタとは別のマシン上でプロセスとして起動しておき、Prometheus はポート `9438`（exporter が listen しているポート）だけをスクレイプします。
どの ECS/ObjectScale クラスタを見るかは、`/probe` に渡す `target` パラメータで指定します。

## 公開されるメトリクス一覧

命名は Prometheus の慣習に従っています。
基本単位（バイト、秒）を使い、カウンタには `_total` サフィックスを付け、単位変換は collector の内部で行います。
すべてのメトリクスに共通するラベルはありません。
`instance` は前述の Prometheus relabel によって付与されるもので、旧 exporter にあった `target_name` ラベルは廃止されました。

### exporter 自身のメタメトリクス

| メトリクス | ラベル | 型 | 説明 |
| --- | --- | --- | --- |
| `obs_scrape_success` | `collector` | Gauge | このスクレイプで該当 collector が成功したかどうか |
| `obs_scrape_duration_seconds` | `collector` | Gauge | 該当 collector が要した時間 |
| `obs_cluster_info` | `vdc`, `version`, `product` | Gauge (=1) | `version` は `/vdc/nodes` から取得した値、`product` は `ecs` または `objectscale` |
| `obs_exporter_build_info` | `version`, `revision`, `goversion` | Gauge (=1) | `/probe` ではなく `/metrics` で公開される |

### cluster collector（`/dashboard/zones/localzone`）

| メトリクス | ラベル | 型 | ソースフィールド |
| --- | --- | --- | --- |
| `obs_cluster_nodes` | `vdc`, `state`=`good\|bad` | Gauge | `numGoodNodes` / `numBadNodes` |
| `obs_cluster_disks` | `vdc`, `state`=`good\|bad` | Gauge | `numGoodDisks` / `numBadDisks` |
| `obs_cluster_capacity_total_bytes` | `vdc` | Gauge | `diskSpaceTotalCurrent.0.Space`（GB→bytes） |
| `obs_cluster_capacity_free_bytes` | `vdc` | Gauge | `diskSpaceFreeCurrent.0.Space`（GB→bytes） |
| `obs_cluster_alerts_unacknowledged` | `vdc`, `severity`=`critical\|error\|info\|warning` | Gauge | `alertsNumUnack*`（旧 exporter の Counter 誤用を修正） |

トランザクションレベルのメトリクス（latency/TPS/bandwidth/errors）は ECS 3.6.0.0 で dashboard API から削除されており、代わりに `perf` collector（Flux API 経由）が提供します。

### replication collector（`/dashboard/zones/localzone/replicationgroups`）

| メトリクス | ラベル | 型 | ソースフィールド |
| --- | --- | --- | --- |
| `obs_replication_ingress_bytes_per_second` | `rg` | Gauge | `replicationIngressTraffic` |
| `obs_replication_egress_bytes_per_second` | `rg` | Gauge | `replicationEgressTraffic` |
| `obs_replication_pending_repo_bytes` | `rg` | Gauge | `chunksRepoPendingReplicationTotalSize` |
| `obs_replication_pending_journal_bytes` | `rg` | Gauge | `chunksJournalPendingReplicationTotalSize` |
| `obs_replication_pending_xor_bytes` | `rg` | Gauge | `chunksPendingXorTotalSize` |
| `obs_replication_rpo_timestamp_seconds` | `rg` | Gauge | `replicationRpoTimestamp`（エポック秒に変換） |

`rg` はレプリケーショングループ名です（旧 exporter ではこれを誤って `node` ラベルとしていました）。

### node collector（DT 統計 / ping。`--collector.node.dt-stats=false` で無効化）

| メトリクス | ラベル | 型 | ソース |
| --- | --- | --- | --- |
| `obs_node_directory_tables` / `obs_node_directory_tables_unready` / `obs_node_directory_tables_unknown` | `node` | Gauge | `/stats/dt/DTInitStat`（XML） |
| `obs_node_active_connections` | `node` | Gauge | `/?ping`（XML） |

### metering collector（明示的に要求された場合のみ実行）

| メトリクス | ラベル | 型 | ソース |
| --- | --- | --- | --- |
| `obs_metering_namespace_quota_bytes` | `namespace`, `quota`=`block\|notification` | Gauge | quota API（GB→bytes） |
| `obs_metering_namespace_used_bytes` | `namespace` | Gauge | billing info の `total_size`（KB→bytes） |
| `obs_metering_namespace_objects` | `namespace` | Gauge | billing info の `total_objects` |

`obs_metering_namespace_quota_bytes` は quota API の `blockSize` が 0 より大きい場合のみ出力されます。
ECS で設定可能な最小のネームスペースクォータは 1GB なので、`blockSize == 0` は「実際のクォータが 0 バイト」ではなく「クォータ未設定」を意味します。

### perf collector（Flux API）

| メトリクス | ラベル | 型 | 説明 |
| --- | --- | --- | --- |
| `obs_perf_read_latency_seconds` | `vdc` | Gauge | 旧 `transactionReadLatencyCurrent`（ms→s） |
| `obs_perf_write_latency_seconds` | `vdc` | Gauge | 旧 `transactionWriteLatencyCurrent`（ms→s） |
| `obs_perf_read_bytes_per_second` | `vdc` | Gauge | 旧 `transactionReadBandwidth` |
| `obs_perf_write_bytes_per_second` | `vdc` | Gauge | 旧 `transactionWriteBandwidth` |
| `obs_perf_read_transactions_per_second` | `vdc` | Gauge | 旧 `transactionReadTransactionsPerSec` |
| `obs_perf_write_transactions_per_second` | `vdc` | Gauge | 旧 `transactionWriteTransactionsPerSec` |
| `obs_perf_transaction_errors` | `vdc`, `category`, `error_type` | Gauge | 旧 `transactionErrors.types`（文字列パースではなく Flux のタグから直接読み取るよう変更） |

perf collector が使う正確な measurement/field の対応関係は `docs/api-research/flux-api-reference.md` を正とします。
latency クエリのタグの意味については、実クラスタでの検証待ちで一部未確定の部分があります。

## 開発者向けの手順

### ビルド手順

```bash
git clone https://github.com/miyamadoKL/prometheus-obs-exporter.git
cd prometheus-obs-exporter
make build     # -> bin/obs-exporter
```

`go.mod` で固定された Go のバージョンが必要です（`GOTOOLCHAIN=auto` により、Go ツールチェインは自動的にダウンロードされます）。

### テストと lint

```bash
make test   # go test -race -cover ./...
make lint   # golangci-lint run ./...
```

### CI/CD

- **CI**（`.github/workflows/ci.yml`）：push と pull request のたびに、ビルド、テスト、lint（`golangci-lint`）を実行します。
- **リリース**（`.github/workflows/release.yml`）：`v*` タグを push すると [GoReleaser](https://goreleaser.com/) v2（`.goreleaser.yml`）が動き、クロスプラットフォームのバイナリ（linux/windows/darwin、amd64/arm64）をビルドして GitHub Releases に公開します。

## Docker Compose での起動

Docker / Docker Compose を使って exporter を起動する手順です。
exporter から対象クラスタの管理 API（デフォルトポート 4443）へ到達できる必要があります。
認証には検証用アカウントを用意し、前述のとおり「System Monitor」ロールを付与してください。

### 認証情報の渡し方

認証情報は環境変数で渡します。

- `OBS_EXPORTER_USERNAME`：ECS/ObjectScale のユーザー名
- `OBS_EXPORTER_PASSWORD`：ECS/ObjectScale のパスワード

シェルの履歴にパスワードを残さないため、次のいずれかの方法を推奨します。

1. `read -s` でプロンプト入力してから `export` します。

   ```bash
   read -s -p "OBS_EXPORTER_PASSWORD: " OBS_EXPORTER_PASSWORD
   export OBS_EXPORTER_PASSWORD
   export OBS_EXPORTER_USERNAME=monitor-user
   ```

2. `.env` ファイルに書き、パーミッションを `600` に制限します。

   ```bash
   cat > .env <<'EOF'
   OBS_EXPORTER_USERNAME=monitor-user
   OBS_EXPORTER_PASSWORD=change-me
   EOF
   chmod 600 .env
   ```

   `.env` ファイルは Git にコミットしないでください。
   `compose.yaml` にはパスワードを直書きしない設計にしてあります。
   `docker compose` は同じディレクトリの `.env` を自動的に読み込みます。

自己署名証明書を使う環境で `--ecs.tls.ca-file` を指定する場合は、前述の「[TLS 設定](#tls-設定)」のとおり、`compose.yaml` の volumes 例のコメントを解除してマウントしてください。

### 起動手順

1. `.env` ファイルを作成します（上記「認証情報の渡し方」を参照）。
2. イメージをビルドして起動します。

   ```bash
   docker compose up -d --build
   ```

3. 起動状況を確認します。

   ```bash
   docker compose ps
   docker compose logs -f obs-exporter
   ```

### 起動後の動作確認

まず exporter 自身の `/metrics` が返ることを確認します。

```bash
curl -s http://localhost:9438/metrics | head
```

次に対象クラスタへの疎通を `/probe` で確認します。
`<ECS_or_OBS_host>` は ECS/ObjectScale クラスタのホスト名または IP に置き換えてください。

```bash
curl -s 'http://localhost:9438/probe?target=<ECS_or_OBS_host>'
```

metering collector を含めて確認する場合は次のようにします。
metering はデフォルトの collector セットに含まれないため、明示的に指定する必要があります。

```bash
curl -s 'http://localhost:9438/probe?target=<ECS_or_OBS_host>&collectors=metering'
```

### 失敗時の切り分け

- HTTP 401 が返る場合：`OBS_EXPORTER_USERNAME` / `OBS_EXPORTER_PASSWORD` を確認してください。
  「System Monitor」ロールが付与されているかも確認してください。
- 証明書エラー（`x509` 関連のエラーメッセージ）が返る場合：`--ecs.tls.ca-file` または `--ecs.tls.insecure-skip-verify` の指定を見直してください。
- タイムアウトする場合：exporter コンテナから対象クラスタの管理 API ポート（4443）へ到達できるか確認してください。
  ファイアウォールやネットワーク経路の問題であることが多いです。

### DT 統計が存在しない環境

一部の環境では DT 統計エンドポイント（`:9101`）が存在しません。
その場合は `node` collector の DT 統計部分を無効化してください。

```bash
--collector.node.dt-stats=false
```

`compose.yaml` では次のように `command` を指定します（該当箇所のコメントを解除してください）。

```yaml
services:
  obs-exporter:
    command: ["--collector.node.dt-stats=false"]
```

## License

Apache License 2.0. See [LICENSE](LICENSE).

This exporter was originally written by [Mark DeNeve](https://github.com/xphyr)
as `prometheus-emcecs-exporter`.
