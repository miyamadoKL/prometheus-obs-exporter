# prometheus-obs-exporter

[![Go Report Card](https://goreportcard.com/badge/github.com/miyamadoKL/prometheus-obs-exporter)](https://goreportcard.com/report/github.com/miyamadoKL/prometheus-obs-exporter)

A Prometheus multi-target exporter for **Dell ObjectScale 4.1.x** and **Dell EMC ECS 3.8.1.x** clusters.

This project is a fork of [paychex/prometheus-emcecs-exporter](https://github.com/paychex/prometheus-emcecs-exporter)
(which targeted ECS 3.3.x), rewritten to support the current dashboard/Flux
APIs of ECS 3.8.1.x and Dell ObjectScale 4.1.x in a single binary. It is not
recommended that you run this tool on an ECS/ObjectScale node itself;
instead run it on a separate machine that can reach the cluster's
management API.

## Breaking changes from prometheus-emcecs-exporter

This is a full rewrite. If you are migrating from `paychex/prometheus-emcecs-exporter`,
expect the following incompatibilities:

- **Metric name prefix changed**: `emcecs_` → `obs_`. All metric names,
  label sets, and metric types were also reworked (see
  [Metrics](#metrics) below); dashboards and alerts built on the old names
  will need to be rewritten.
- **Scrape endpoint changed**: `/query` → `/probe`. Update your
  `scrape_configs` `metrics_path`.
- **Flag and environment variable scheme changed**: flags now use dotted
  `--ecs.*` / `--collector.*` / `--web.*` namespaces (via
  [kingpin](https://github.com/alecthomas/kingpin)), and environment
  variables use the `OBS_EXPORTER_*` prefix instead of `ECSENV_*`. See the
  [Configuration](#configuration) table below for the full mapping.
- **`metering` collector must be explicitly requested**: pass
  `collectors=metering` (or include it alongside other collectors) on the
  `/probe` query string. There is no longer a boolean `metering=1` toggle,
  and metering is no longer part of the default collector set because it
  scales with the number of namespaces and can be slow.
- **TLS verification is opt-in**: the old exporter always skipped TLS
  verification when talking to ECS. This exporter verifies certificates by
  default; use `--ecs.tls.insecure-skip-verify` or `--ecs.tls.ca-file` to
  restore the old behavior or trust a self-signed certificate.
- **CI/release tooling replaced**: Travis CI and the old `goreleaser` v0
  config are gone, replaced by GitHub Actions and `goreleaser` v2 (see
  [Development](#development)).

## Configuration

All settings can be provided as a command-line flag or the corresponding
environment variable.

| Flag | Environment variable | Default | Notes |
| --- | --- | --- | --- |
| `--web.listen-address` | `OBS_EXPORTER_WEB_LISTEN_ADDRESS` | `:9438` | Standard `exporter-toolkit` web flag; repeatable |
| `--web.config.file` | — | (none) | TLS/Basic-auth configuration for the exporter's own listener ([exporter-toolkit web config](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md)) |
| `--ecs.username` | `OBS_EXPORTER_USERNAME` | (none, required) | ECS/ObjectScale username. **System Monitor** role recommended |
| `--ecs.password` | `OBS_EXPORTER_PASSWORD` | (none, required) | ECS/ObjectScale password |
| `--ecs.mgmt-port` | `OBS_EXPORTER_MGMT_PORT` | `4443` | Management API port; also used for the metering API (fixes a hardcoded-port bug present in the old exporter) |
| `--ecs.obj-port` | `OBS_EXPORTER_OBJ_PORT` | `9021` | Object (S3) API port, used for the node ping check |
| `--ecs.tls.insecure-skip-verify` | `OBS_EXPORTER_TLS_INSECURE_SKIP_VERIFY` | `false` | Skip TLS certificate verification against ECS/ObjectScale. Opt-in (the old exporter always skipped verification) |
| `--ecs.tls.ca-file` | `OBS_EXPORTER_TLS_CA_FILE` | (none) | PEM CA bundle to verify a self-signed ECS/ObjectScale certificate |
| `--collector.node.dt-stats` | `OBS_EXPORTER_COLLECTOR_NODE_DT_STATS` | `true` | Enable the DT statistics / ping node collector. Disable if `:9101` no longer exists on your platform |
| `--collector.metering.concurrency` | `OBS_EXPORTER_METERING_CONCURRENCY` | `4` | Maximum concurrent requests issued against the metering API |
| `--collector.perf.range` | `OBS_EXPORTER_PERF_RANGE` | `5m` | Flux query range for the perf collector (maximum `1h` per the Flux API) |
| `--log.level` | — | `info` | Standard `promslog` flag |
| `--log.format` | — | `logfmt` | Standard `promslog` flag |

Run `obs-exporter --help` for the full, authoritative list of flags.

## Required ECS / ObjectScale role

The configured user only needs read access to cluster health, replication,
node, and metering data. The **System Monitor** built-in role is
recommended; it does not grant any provisioning or administrative
capability.

## TLS

By default the exporter verifies the TLS certificate presented by the
ECS/ObjectScale management API. If your cluster uses a self-signed
certificate, choose one of:

- `--ecs.tls.ca-file=/path/to/ca.pem` — trust a specific CA (recommended)
- `--ecs.tls.insecure-skip-verify` — disable verification entirely (not
  recommended outside of lab/test environments)

The exporter's own HTTP listener can independently be configured with TLS
and/or Basic auth via `--web.config.file`; see the
[exporter-toolkit web configuration docs](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md).

## The `/probe` endpoint

```
GET /probe?target=<host>[&collectors=cluster,replication,node,perf,metering]
```

- `target` (required): the ECS/ObjectScale cluster hostname or IP to
  scrape.
- `collectors` (optional, comma-separated): which collectors to run for
  this scrape. Defaults to `cluster,replication,node,perf`.
  - `metering` is **not** included by default because it scales with the
    number of namespaces on the cluster and can take a long time; request
    it explicitly, ideally on its own, lower-frequency scrape job.

Other endpoints:

- `GET /metrics` — the exporter's own process/build metrics
  (`obs_exporter_build_info`, Go runtime stats, etc.)
- `GET /` — a landing page with links to the endpoints above

Authenticated clients are cached per target for the lifetime of the
process; on `SIGTERM`/`SIGINT` the exporter logs out of every cached
target (ECS/ObjectScale caps active tokens at 100 per user).

## Prometheus configuration

This exporter follows the multi-target exporter pattern (the same pattern
used by the [SNMP exporter](https://github.com/prometheus/snmp_exporter)
and [blackbox exporter](https://github.com/prometheus/blackbox_exporter)):
one exporter process can scrape many ECS/ObjectScale clusters. Each
cluster is a `target` on the `/probe` query string, and `relabel_configs`
rewrites `instance` to the target name.

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

  # Metering (namespace quota/usage) metrics: heavier and slower-changing,
  # so it gets its own low-frequency job with `collectors=metering`.
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

  # The exporter's own process/build metrics.
  - job_name: "obs-exporter-self"
    static_configs:
      - targets:
          - 127.0.0.1:9438
```

Very large clusters may need a higher-than-default `scrape_timeout`.

## Metrics

Naming follows Prometheus conventions: base units (bytes, seconds),
counters suffixed `_total`, and unit conversions performed inside the
collector. There is no shared label across all metrics — `instance` is
attached by Prometheus relabeling (see above); the old exporter's
`target_name` label is gone.

### Meta

| Metric | Labels | Type | Description |
| --- | --- | --- | --- |
| `obs_scrape_success` | `collector` | Gauge | Whether the given collector succeeded for this scrape |
| `obs_scrape_duration_seconds` | `collector` | Gauge | Time taken by the given collector |
| `obs_cluster_info` | `vdc`, `version`, `product` | Gauge (=1) | `version` comes from `/vdc/nodes`; `product` is `ecs` or `objectscale` |
| `obs_exporter_build_info` | `version`, `revision`, `goversion` | Gauge (=1) | Exposed on `/metrics`, not `/probe` |

### cluster collector (`/dashboard/zones/localzone`)

| Metric | Labels | Type | Source field |
| --- | --- | --- | --- |
| `obs_cluster_nodes` | `vdc`, `state`=`good\|bad` | Gauge | `numGoodNodes` / `numBadNodes` |
| `obs_cluster_disks` | `vdc`, `state`=`good\|bad` | Gauge | `numGoodDisks` / `numBadDisks` |
| `obs_cluster_capacity_total_bytes` | `vdc` | Gauge | `diskSpaceTotalCurrent.0.Space` (GB→bytes) |
| `obs_cluster_capacity_free_bytes` | `vdc` | Gauge | `diskSpaceFreeCurrent.0.Space` (GB→bytes) |
| `obs_cluster_alerts_unacknowledged` | `vdc`, `severity`=`critical\|error\|info\|warning` | Gauge | `alertsNumUnack*` (fixes the old exporter's Counter misuse) |

Transaction-level metrics (latency/TPS/bandwidth/errors) were removed from
the dashboard API in ECS 3.6.0.0 and are provided by the `perf` collector
(via the Flux API) instead.

### replication collector (`/dashboard/zones/localzone/replicationgroups`)

| Metric | Labels | Type | Source field |
| --- | --- | --- | --- |
| `obs_replication_ingress_bytes_per_second` | `rg` | Gauge | `replicationIngressTraffic` |
| `obs_replication_egress_bytes_per_second` | `rg` | Gauge | `replicationEgressTraffic` |
| `obs_replication_pending_repo_bytes` | `rg` | Gauge | `chunksRepoPendingReplicationTotalSize` |
| `obs_replication_pending_journal_bytes` | `rg` | Gauge | `chunksJournalPendingReplicationTotalSize` |
| `obs_replication_pending_xor_bytes` | `rg` | Gauge | `chunksPendingXorTotalSize` |
| `obs_replication_rpo_timestamp_seconds` | `rg` | Gauge | `replicationRpoTimestamp` (converted to epoch seconds) |

`rg` is the replication group name (the old exporter mislabeled this as
`node`).

### node collector (DT statistics / ping; disable with `--collector.node.dt-stats=false`)

| Metric | Labels | Type | Source |
| --- | --- | --- | --- |
| `obs_node_directory_tables` / `obs_node_directory_tables_unready` / `obs_node_directory_tables_unknown` | `node` | Gauge | `/stats/dt/DTInitStat` (XML) |
| `obs_node_active_connections` | `node` | Gauge | `/?ping` (XML) |

### metering collector (only runs when explicitly requested)

| Metric | Labels | Type | Source |
| --- | --- | --- | --- |
| `obs_metering_namespace_quota_bytes` | `namespace`, `quota`=`block\|notification` | Gauge | quota API (GB→bytes) |
| `obs_metering_namespace_used_bytes` | `namespace` | Gauge | billing info `total_size` (KB→bytes) |
| `obs_metering_namespace_objects` | `namespace` | Gauge | billing info `total_objects` |

`obs_metering_namespace_quota_bytes` is only emitted when the quota API's `blockSize` is greater than 0: ECS's minimum configurable namespace quota is 1GB, so `blockSize == 0` means "no quota configured", not an actual zero-byte quota.

### perf collector (Flux API)

| Metric | Labels | Type | Description |
| --- | --- | --- | --- |
| `obs_perf_read_latency_seconds` | `vdc` | Gauge | formerly `transactionReadLatencyCurrent` (ms→s) |
| `obs_perf_write_latency_seconds` | `vdc` | Gauge | formerly `transactionWriteLatencyCurrent` (ms→s) |
| `obs_perf_read_bytes_per_second` | `vdc` | Gauge | formerly `transactionReadBandwidth` |
| `obs_perf_write_bytes_per_second` | `vdc` | Gauge | formerly `transactionWriteBandwidth` |
| `obs_perf_read_transactions_per_second` | `vdc` | Gauge | formerly `transactionReadTransactionsPerSec` |
| `obs_perf_write_transactions_per_second` | `vdc` | Gauge | formerly `transactionWriteTransactionsPerSec` |
| `obs_perf_transaction_errors` | `vdc`, `category`, `error_type` | Gauge | formerly `transactionErrors.types` (now read directly from Flux tags instead of parsed from a string) |

The exact measurement/field mapping used by the perf collector is the
`docs/api-research/flux-api-reference.md` document, which is the source of
truth; some of the latency query's tag semantics are unconfirmed pending
verification against a live cluster.

## Development

### Building

```bash
git clone https://github.com/miyamadoKL/prometheus-obs-exporter.git
cd prometheus-obs-exporter
make build     # -> bin/obs-exporter
```

Requires the Go version pinned in `go.mod` (Go toolchain auto-download via
`GOTOOLCHAIN=auto` handles this for you).

### Testing and linting

```bash
make test   # go test -race -cover ./...
make lint   # golangci-lint run ./...
```

### CI/CD

- **CI** (`.github/workflows/ci.yml`): every push and pull request builds,
  tests, and lints (`golangci-lint`) the project.
- **Release** (`.github/workflows/release.yml`): pushing a `v*` tag runs
  [GoReleaser](https://goreleaser.com/) v2 (`.goreleaser.yml`) to build and
  publish cross-platform binaries (linux/windows/darwin,
  amd64/arm64) to GitHub Releases.

## 接続設定（日本語）

このセクションでは Docker / Docker Compose での起動方法と、ECS/ObjectScale への接続設定を日本語で説明する。

### 前提条件

対象は ECS 3.8.1.x または ObjectScale 4.1.x である。
exporter から対象クラスタの管理 API（デフォルトポート 4443）へ到達できる必要がある。
認証には検証用アカウントを用意する。
ロールは **System Monitor** を推奨する。
このロールは読み取り専用で、プロビジョニングや管理操作の権限を持たない。

### 認証情報の渡し方

認証情報は環境変数で渡す。

- `OBS_EXPORTER_USERNAME`: ECS/ObjectScale のユーザー名
- `OBS_EXPORTER_PASSWORD`: ECS/ObjectScale のパスワード

シェルの履歴にパスワードを残さないため、次のいずれかの方法を推奨する。

1. `read -s` でプロンプト入力してから `export` する。

   ```bash
   read -s -p "OBS_EXPORTER_PASSWORD: " OBS_EXPORTER_PASSWORD
   export OBS_EXPORTER_PASSWORD
   export OBS_EXPORTER_USERNAME=monitor-user
   ```

2. `.env` ファイルに書き、パーミッションを `600` に制限する。

   ```bash
   cat > .env <<'EOF'
   OBS_EXPORTER_USERNAME=monitor-user
   OBS_EXPORTER_PASSWORD=change-me
   EOF
   chmod 600 .env
   ```

   `.env` ファイルは Git にコミットしないこと。
   `compose.yaml` にはパスワードを直書きしない設計にしてある。
   `docker compose` は同じディレクトリの `.env` を自動的に読み込む。

### TLS 設定

自己署名証明書を使う環境では、デフォルトの TLS 検証がそのままでは失敗する。
次のいずれかを指定する。

- `--ecs.tls.ca-file`（環境変数 `OBS_EXPORTER_TLS_CA_FILE`）: クラスタの CA 証明書を信頼する。推奨する方法である。
- `--ecs.tls.insecure-skip-verify`（環境変数 `OBS_EXPORTER_TLS_INSECURE_SKIP_VERIFY=true`）: 証明書検証を無効化する。検証用途に限定し、本番環境では使わないこと。

CA ファイルを使う場合は、`compose.yaml` の volumes 例のコメントを解除してマウントする。

### Docker Compose での起動手順

1. `.env` ファイルを作成する（上記「認証情報の渡し方」を参照）。
2. イメージをビルドして起動する。

   ```bash
   docker compose up -d --build
   ```

3. 起動状況を確認する。

   ```bash
   docker compose ps
   docker compose logs -f obs-exporter
   ```

### 動作確認

まず exporter 自身の `/metrics` が返ることを確認する。

```bash
curl -s http://localhost:9438/metrics | head
```

次に対象クラスタへの疎通を `/probe` で確認する。
`<ECS_or_OBS_host>` は ECS/ObjectScale クラスタのホスト名または IP に置き換える。

```bash
curl -s 'http://localhost:9438/probe?target=<ECS_or_OBS_host>'
```

metering collector を含めて確認する場合は次のようにする。
metering はデフォルトの collector セットに含まれないため、明示的に指定する必要がある。

```bash
curl -s 'http://localhost:9438/probe?target=<ECS_or_OBS_host>&collectors=metering'
```

### 失敗時の切り分け

- HTTP 401 が返る場合: `OBS_EXPORTER_USERNAME` / `OBS_EXPORTER_PASSWORD` を確認する。System Monitor ロールが付与されているかも確認する。
- 証明書エラー（`x509` 関連のエラーメッセージ）が返る場合: `--ecs.tls.ca-file` または `--ecs.tls.insecure-skip-verify` の指定を見直す。
- タイムアウトする場合: exporter コンテナから対象クラスタの管理 API ポート（4443）へ到達できるか確認する。ファイアウォールやネットワーク経路の問題であることが多い。

### DT 統計が存在しない環境

一部の環境では DT 統計エンドポイント（`:9101`）が存在しない。
その場合は `node` collector の DT 統計部分を無効化する。

```bash
--collector.node.dt-stats=false
```

`compose.yaml` では次のように `command` を指定する（該当箇所のコメントを解除する）。

```yaml
services:
  obs-exporter:
    command: ["--collector.node.dt-stats=false"]
```

## License

Apache License 2.0. See [LICENSE](LICENSE).

This exporter was originally written by [Mark DeNeve](https://github.com/xphyr)
as `prometheus-emcecs-exporter`.
