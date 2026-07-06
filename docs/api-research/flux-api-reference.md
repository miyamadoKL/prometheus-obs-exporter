# Dell ECS / ObjectScale Flux API リファレンス

## 出典表記について

本書では以下のエイリアスで出典元マニュアルを表す。

- `ECS_DOC`＝`/home/nagata_yasuhiro/miyamado/user-manuals/dell-ecs/monitoring-guide/04-advanced-monitoring.md`（Dell ECS 3.8 監視ガイド 第4章、全1980行）
- `OS_DOC`＝`/home/nagata_yasuhiro/miyamado/user-manuals/dell-objectscale/admin-guide/15-advanced-monitoring.md`（Dell ObjectScale 4.x 管理ガイド 第15章、全1742行）

出典は `(出典: ECS_DOC:行番号)` の形式で付記する。
両マニュアルに同内容の記述がある場合は両方の出典を付記する。

マニュアルに明記されていない情報は「記載なし」と明記し、推測や一般知識で埋めない。
表27（ECS版の呼称。OS版には相当する表番号自体が存在しない）のように、マニュアルの記述が database 単位までしか対応関係を示していない箇所については、他の節の measurement カタログと突き合わせた「推定」を別途明示し、確定情報と混同しないようにする。

## Flux API 調査の目的と対象マニュアル

本書の目的は、`prometheus-obs-exporter` を Dell ECS 3.8 / ObjectScale 4.x に対応させる改修のため、ECS 3.6.0.0 以降で dashboard API（管理系 REST API）から削除されたフィールドを Flux API（Grafana 高度な監視ダッシュボードが利用する時系列 DB への Flux クエリ言語 REST API）経由で取得する方法を、両マニュアルの記述に基づき1つのリファレンスに集約することである。

対象とするマニュアルとバージョンは、次のとおりである。

- **ECS_DOC**：Dell ECS 3.8 系「監視ガイド」第4章「高度な監視」
- **OS_DOC**：Dell ObjectScale 4.x 系「管理ガイド」第15章「高度な監視」

両マニュアルは記述の大部分（Flux API の呼び出し仕様、`monitoring_main` / `monitoring_last` / `monitoring_op` / `monitoring_vdc` の measurement カタログ）がほぼ同一である一方、ECS_DOC にのみ存在し OS_DOC には存在しない章立てがある。
特に、削除フィールドの対応表（表27）、削除 API 一覧（表28）、「非推奨 Dashboard API に対する Flux API の代替」節（procstat の `process_name` タグ値の列挙、`range` 上限1時間の明記を含む）は ECS_DOC 固有であり、OS_DOC には対応する記述が存在しない。
この点は、OS_DOC 全1742行を通読し、テキスト検索でも「表27」相当の見出し、「process_name」のタグ値列挙、「range」の上限に関する記述、「Dashboard API」非推奨化に関する節がいずれも存在しないことを確認した。
この非対称性は本改修の設計上重要な制約であるため、6章で詳述する。

---

## 1. Flux API 呼び出し仕様

### 1.1 Flux API の全体像

Flux API は、curl で REST クエリを送信して時系列データベース（`fluxd` サービス）から生データを取得する仕組みである。
Dashboard API の使用方法と同様に `fluxd` サービスからデータを取得するが、事前にトークンを取得し、リクエストヘッダにそのトークンを提供する必要がある。
(出典: ECS_DOC:370-372, OS_DOC:423-425)

### 1.2 前提条件（ロール）

以下のいずれかのロールが必要である。

- `SYSTEM_ADMIN`
- `SYSTEM_MONITOR`

(出典: ECS_DOC:376-379, OS_DOC:429-432)

### 1.3 認証方式

1. ログイン API にベーシック認証でリクエストし、レスポンスヘッダから `X-SDS-AUTH-TOKEN` を取得する。

   ```text
   admin@ecs:> tok=$(curl -iks https://localhost:4443/login -u emcmonitor:#### | grep X-SDS-AUTH-TOKEN)
   admin@ecs:/> echo $tok
   X-SDS-AUTH-TOKEN:****
   ```

   `####` はパスワードを表す。
   `****` は `X-SDS-AUTH-TOKEN` の値を表す。
   (出典: ECS_DOC:402-415, OS_DOC:455-465)

   OS_DOC でもプロンプト表記が `admin@ecs:>` のままであり、ECS 由来の記述がそのまま流用されている可能性がある。
   ObjectScale 側での実際のログインエンドポイントのホスト名、ポート、ユーザー名が同一かどうかは、本文からは判断できない。

2. 取得したトークン `$tok` を、以後の Flux API リクエストの HTTP ヘッダとしてそのまま付与する（`-H "$tok"`）。

**記載なし**：トークンの有効期限、リフレッシュ方法、`emcmonitor` 以外のユーザーでの認証可否、Basic 認証をトークン取得以外の用途（Flux クエリ本体への直接付与）に使えるかどうかは、両マニュアルとも明記されていない。

### 1.4 エンドポイントと HTTP メソッド

- URL：`https://localhost:4443/flux/api/external/v2/query`
- メソッド：`POST`（`-XPOST`）

(出典: ECS_DOC:427, ECS_DOC:509, OS_DOC:475, OS_DOC:528)

`localhost:4443` は curl 実行元がノード上（`admin@ecs:` プロンプト）であることを前提としている。
外部クライアントから実行する場合の実際のホスト名、ポート、TLS証明書検証要否（`-k` は自己署名証明書許容のためのオプション）について、両マニュアルとも「これがそのまま外部からの疎通に使える」とは明言していない。
**記載なし**：外部（ECS/ObjectScale ノード外）から Flux API を呼び出す際の到達可能なエンドポイント（ロードバランサ経由か、管理ノードIP直打ちか等）。

### 1.5 リクエストヘッダ

| 出力形式 | ヘッダ |
|---|---|
| JSON | `-H "$tok"` / `-H 'accept:application/json'` / `-H 'content-type:application/json'` |
| CSV | `-H "$tok"` / `-H 'accept:application/csv'` / `-H 'content-type:application/vnd.flux'` |

(出典: ECS_DOC:392-398, 427-428, 509-510; OS_DOC:445-451, 475-476, 528-529)

### 1.6 リクエストボディ

- JSON 形式の場合：JSON オブジェクトで `query` キーに Flux クエリ文字列を格納する。

  ```json
  {
  "query": "from(bucket:\"monitoring_main\") |> range(start: -30m) |> filter(fn: (r) =>
  r._measurement == \"statDataHead_performance_internal_transactions\")"
  }
  ```
  (出典: ECS_DOC:383-390, OS_DOC:436-443)

- `application/vnd.flux`（CSV 出力）の場合：Flux クエリ文字列を生のリクエストボディとしてそのまま送信する（JSON でラップしない）。

  ```flux
  query=from(bucket: "monitoring_main")
  |> range(start: -30m)
  |> filter(fn: (r) => r._measurement == "statDataHead_performance_internal_transactions")
  ```
  (出典: ECS_DOC:392-398, OS_DOC:445-451)

  ただし実際の CSV curl 実行例（1.7節）では `query=` プレフィックスを付けず、Flux クエリ文字列をそのまま `-d` に渡している。
  この「ペイロード例」節と「実行例」節の間で表記に軽微な不整合があるが、両マニュアルとも同じ不整合を含んでいる。

### 1.7 実行例（curl コマンド、マニュアルより原文引用）

**JSON 出力の例**

```text
admin@ecs:/> curl https://localhost:4443/flux/api/external/v2/query -XPOST -k -sS -H
"$tok" -H 'accept:application/json' -H 'content-type:application/json' -d '{
"query": "from(bucket:\"monitoring_main\") |> range(start: -30m) |> filter(fn: (r) =>
r._measurement == \"statDataHead_performance_internal_transactions\")" }'
```
(出典: ECS_DOC:427-431, OS_DOC:475-479)

**CSV 出力の例**

```text
admin@ecs:> curl https://localhost:4443/flux/api/external/v2/query -XPOST -k -sS -H
"$tok" -H 'accept:application/csv' -H 'content-type:application/vnd.flux' -d
'from(bucket:"monitoring_main") |> range(start: -30m) |> filter(fn: (r) => r._measurement
== "statDataHead_performance_internal_transactions")'
```
(出典: ECS_DOC:509-512, OS_DOC:528-531)

### 1.8 レスポンス形式

**JSON レスポンス**：`Series` 配列の各要素が `Datatypes`（各列の型）、`Columns`（列名）、`Values`（行データの配列の配列、すべて文字列表現）を持つ。

```json
{
"Series": [
{
"Datatypes": [
"long","dateTime:RFC3339","dateTime:RFC3339","dateTime:RFC3339","long",
"string","string","string","string","string","string"
],
"Columns": [
"table","_start","_stop","_time","_value","_field","_measurement",
"host","node_id","process","tag"
],
"Values": [
["0","2020-03-10T09:54:31.2077998552","2020-03-10T10:24:31.2077998552",
"2020-03-10T09:56:43Z","1","failed_request_counter",
"statDataHead_performance_internal_transactions","ecs.lss.emc.com",
"28cd473e-ca45-4623-b30d-0481c548a650","statDataHead","dashboard"]
]
}
]
}
```
(出典: ECS_DOC:433-503, OS_DOC:479-522)

`Columns` の並びはクエリ対象 measurement のタグ構成によって変わる。
この例は `statDataHead_performance_internal_transactions` の場合である。

**CSV レスポンス**：InfluxDB/Flux 標準のアノテーション付き CSV（`#datatype` / `#group` / `#default` のメタ行とヘッダ行、データ行）。

```text
#datatype,string,long,dateTime:RFC3339,dateTime:RFC3339,long,string,string,string,string,string,string
#group,false,false,false,false,false,true,true,true,true,true
#default,_result,,,,,,,,,,
,result,table,_start,_stop,_time,_value,_field,_measurement,host,node_id,process,tag
,,0,2020-03-10T09:58:59.049910533Z,2020-03-10T10:28:59.049910533Z,2020-03-10T10:01:43Z,1,
failed_request_counter,statDataHead_performance_internal_transactions,ecs.lss.emc.com,28c
d473e-ca45-4623-b30d-0481c548a650,statDataHead,dashboard
```
(出典: ECS_DOC:513-530, OS_DOC:532-545)

### 1.9 共通タグ

すべての measurement に共通するタグ値は、次のとおりである。

- `host`：データノードの名前
- `node_id`：データノードの ID
- `tag`：内部用、常に `dashboard` に設定される

(出典: ECS_DOC:534-538, OS_DOC:549-553)

パフォーマンス系 measurement（`monitoring_main` / `monitoring_vdc` の performance 系）に共通するタグ値は、次のとおりである。

- `process`：内部用、`statDataHead` に設定される
- `head`：プロトコルのタイプ（例: S3）
- `namespace`：Namespace の名前
- `method`：プロトコル固有のリクエストメソッド（`GET`, `POST`, `READ`, `WRITE`）

(出典: ECS_DOC:1338-1345, OS_DOC:1367-1374)

---

## 2. 削除フィールド対応表（表27相当）

### 2.1 表の所在

ECS_DOC には「表27. 削除されたデータの代替場所」という明示的な表があり、`Dashboard APIs` 節「ECS 3.6.0.0 で変更された API」の直後に置かれている。
(出典: ECS_DOC:1908-1969)

OS_DOC にはこの表に相当する記述が存在しない。
表番号どころか、削除フィールドの対応表そのもの、「Dashboard APIs」という章立て、「ECS 3.6.0.0 で変更された API」の一覧、削除された API の一覧（表28相当）のいずれも OS_DOC には見つからなかった（OS_DOC 全1742行を通読、grep 双方で確認済み）。

### 2.2 ECS 3.6.0.0 で dashboard API から削除されたフィールド一覧

```text
nodeCpuUtilization*, nodeMemoryUtilizationBytes*, nodeMemoryUtilization*,
nodeNicBandwidth*, nodeNicReceivedBandwidth*, nodeNicTransmittedBandwidth*
nodeNicUtilization*, nodeNicReceivedUtilization*, nodeNicTransmittedUtilization*
capacityRebalanceEnabled, capacityRebalanced, capacityPendingRebalancing
capacityRebalancedAvg, capacityRebalanceRate, capacityPendingRebalancingAvg
transactionReadLatency, transactionWriteLatency, transactionReadBandwidth,
transactionWriteBandwidth
transactionReadTransactionsPerSec, transactionWriteTransactionsPerSec,
transactionErrors.*
diskReadBandwidthTotal, diskWriteBandwidthTotal, diskReadBandwidthEc,
diskWriteBandwidthEc
diskReadBandwidthCc, diskWriteBandwidthCc, diskReadBandwidthRecovery,
diskWriteBandwidthRecovery
diskReadBandwidthGeo, diskWriteBandwidthGeo, diskReadBandwidthUser,
diskWriteBandwidthUser, diskReadBandwidthXor, diskWriteBandwidthXor
```
(出典: ECS_DOC:1921-1939。対象 API: `/dashboard/zones/localzone`, `/dashboard/zones/localzone/nodes`, `/dashboard/nodes/{id}`, `/dashboard/storagepools/{id}/nodes`。ECS_DOC:1913-1919)

マニュアルの注意書き：「すべての削除データに直接の代替があるわけではない。削除されたデータの一部は、他のメトリクスに基づいて計算する必要がある。」
(出典: ECS_DOC:1946)

### 2.3 表27の内容（原文転記、ECS_DOCのみ）

| 番号 | 項目 | 削除されたデータ | 代替の場所（database） | Measurement / Field |
|---|---|---|---|---|
| 1 | ノードシステムレベルデータ | `nodeCpuUtilization*`, `nodeMemoryUtilizationBytes*`, `nodeMemoryUtilization*`, `nodeNicBandwidth*`, `nodeNicReceivedBandwidth*`, `nodeNicTransmittedBandwidth*`, `nodeNicUtilization*`, `nodeNicReceivedUtilization*`, `nodeNicTransmittedUtilization*` | `monitoring_op`（メトリクスの監視リスト: 非パフォーマンス > ノードシステムレベル統計） | Measurement: `cpu`, `mem`, `net` |
| 2.1 | リバランス関連データ | `capacityRebalanced`, `capacityPendingRebalancing`, `capacityRebalancedAvg`, `capacityRebalanceRate`, `capacityPendingRebalancingAvg` | `monitoring_vdc`（メトリクスの監視リスト: 非パフォーマンス） | Measurement: `cq_node_rebalancing_summary` |
| 2.2 | リバランス関連データ | `capacityRebalanceEnabled` | `monitoring_last`（構成フレームワーク値のエクスポート） | Measurement: `dtquery_cmf`, Field: `com.emc.ecs.chunk.rebalance.is_enabled` (integer) |
| 3 | トランザクション関連データ | `transactionReadLatency`, `transactionWriteLatency`, `transactionReadBandwidth`, `transactionWriteBandwidth`, `transactionReadTransactionsPerSec`, `transactionWriteTransactionsPerSec`, `transactionErrors*` | VDC値: `monitoring_vdc`（メトリクスの監視リスト: パフォーマンス）。ノード値: `monitoring_main`（メトリクスの監視リスト: パフォーマンス） | 表27上では database レベルの参照のみで、measurement/field 単位の対応は明記されていない（2.4節で推定を補足） |
| 4 | ディスク関連データ | `diskReadBandwidthTotal`, `diskWriteBandwidthTotal`, `diskReadBandwidthEc`, `diskWriteBandwidthEc`, `diskReadBandwidthCc`, `diskWriteBandwidthCc`, `diskReadBandwidthRecovery`, `diskWriteBandwidthRecovery`, `diskReadBandwidthGeo`, `diskWriteBandwidthGeo`, `diskReadBandwidthUser`, `diskWriteBandwidthUser`, `diskReadBandwidthXor`, `diskWriteBandwidthXor` | VDC値: `monitoring_vdc`（非パフォーマンス）。ノード値: `monitoring_main`（ECS サービス I/O 統計のデータ） | Measurement: `cq_disk_bandwidth`（VDC）/ `<service>_IO_Statistics_data_read` \| `_write`（ノード） |

(出典: ECS_DOC:1948-1969)

### 2.4 トランザクション系フィールドの詳細対応（推定・要検証）

表27の行3は database 単位の参照（「メトリクスの監視リスト: パフォーマンス」節を見よ、とのみ記載）にとどまり、フィールド単位の対応は本文に明記されていない。
以下は、同マニュアルの「メトリクスの監視リスト: パフォーマンス」節（3章参照）に掲載されている measurement カタログの命名、フィールド構成と、削除フィールド名（read/write, latency/bandwidth/TPS/errors）を突き合わせて導いた推定であり、マニュアルが明示的に保証する対応表ではない。
exporter 実装時は実機での検証が必須である。

| 削除フィールド | 対応すると推定される Flux measurement | database | field | tag | 確度 |
|---|---|---|---|---|---|
| `transactionReadBandwidth` | `cq_performance_throughput`（VDC） / `statDataHead_performance_internal_throughput`（ノード） | `monitoring_vdc` / `monitoring_main` | `total_read_requests_size` | VDC: なし。ノード: `host`, `node_id`, `process`, `tag` | 高（フィールド名が read/write で明確に分離されている） |
| `transactionWriteBandwidth` | 同上 | 同上 | `total_write_requests_size` | 同上 | 高 |
| `transactionReadTransactionsPerSec` | `cq_performance_transaction_method`（VDC、`method` タグで `READ` に絞り込み） / `statDataHead_performance_internal_transactions_method`（ノード、同様） | `monitoring_vdc` / `monitoring_main` | `succeed_request_counter`（成功分）, `failed_request_counter`（失敗分） | `method`（値 `READ` でフィルタ） | 中（`method` タグの値が `GET/POST/READ/WRITE` と明記されているが、`READ`＝read transaction という対応はマニュアルが直接述べているわけではなく、1.9節の method タグ説明からの類推） |
| `transactionWriteTransactionsPerSec` | 同上（`method == "WRITE"` でフィルタ） | 同上 | 同上 | `method`（値 `WRITE`） | 中 |
| `transactionReadLatency` | `cq_performance_latency`（VDC） / `statDataHead_performance_internal_latency`（ノード） | `monitoring_vdc` / `monitoring_main` | `p50`, `p99`（VDC のみ。ノード側はヒストグラムバケット値） | `id`（ヒストグラムバケット識別、値の一覧は**記載なし**） | 低（read/write を分離するタグ、フィールドが measurement 定義上に見当たらない。`id` タグの値一覧が両マニュアルとも記載されておらず、read latency と write latency をどう区別するのか本文からは判断できない） |
| `transactionWriteLatency` | 同上 | 同上 | 同上 | 同上 | 低（同上の理由） |
| `transactionErrors.*` | `cq_performance_error*` 系（VDC） / `statDataHead_performance_internal_error*` 系（ノード） | `monitoring_vdc` / `monitoring_main` | `system_errors`, `user_errors`（種別別）、`error_counter`（`cq_performance_error_code` / `statDataHead_performance_internal_error_code`、`code` タグでエラーコード別） | `code`（エラーコード別の場合）, `head`（プロトコル別の場合）, `namespace`（Namespace別の場合） | 中〜高（`system_errors`/`user_errors` という2分類が Data Access Performance ダッシュボードの「System Failures」「User Failures」フィールドと名称上一致しており、対応は妥当性が高い） |

**記載なし**：`transactionReadLatency`/`transactionWriteLatency` について、read/write を区別する具体的なクエリ方法（タグフィルタ）。
`cq_performance_latency` / `statDataHead_performance_internal_latency` の `id` タグが取りうる値の一覧。

### 2.5 ディスク系フィールドの詳細対応（表27より読み取り可能、確度: 高）

表27行4は database までしか明記していないが、`cq_disk_bandwidth`（2.6節）と `<service>_IO_Statistics_data_read`/`_write`（4章に相当する monitoring_main のI/O統計、3章参照）のフィールド名が削除フィールド名とほぼ1対1で対応するため、確度は高い。

| 削除フィールド | VDC: `cq_disk_bandwidth` の field（`type_op` タグで read/write を選択） | ノード: `<service>_IO_Statistics_data_read`/`_write` の field |
|---|---|---|
| `diskReadBandwidthTotal` / `diskWriteBandwidthTotal` | `total`（`type_op="read"` / `"write"`） | 該当フィールドなし（ノード側は個別カテゴリ別のみで total 相当フィールドはノードレベルの IO 統計には存在しない） |
| `diskReadBandwidthEc` / `diskWriteBandwidthEc` | `erasure_encoding` | `read_ECTotal` / `write_ECTotal` |
| `diskReadBandwidthCc` / `diskWriteBandwidthCc` | `consistency_checker` | `read_CCTotal` / `write_CCTotal` |
| `diskReadBandwidthRecovery` / `diskWriteBandwidthRecovery` | `hardware_recovery` | `read_RECOVERTotal` / `write_RECOVERTotal` |
| `diskReadBandwidthGeo` / `diskWriteBandwidthGeo` | `geo` | `read_GEOTotal` / `write_GEOTotal` |
| `diskReadBandwidthUser` / `diskWriteBandwidthUser` | `user_traffic` | `read_USERTotal` / `write_USERTotal` |
| `diskReadBandwidthXor` / `diskWriteBandwidthXor` | `xor` | `read_XORTotal` / `write_XORTotal` |

(出典: `cq_disk_bandwidth` 定義 ECS_DOC:1300-1310, OS_DOC:1330-1340。`<service>_IO_Statistics_data_read`/`_write` 定義 ECS_DOC:584-606, OS_DOC:608-630)

### 2.6 `cq_disk_bandwidth` measurement 定義

```text
Measurement: cq_disk_bandwidth
Tags: type_op ('read', 'write')
Fields: consistency_checker (float)
        erasure_encoding (float)
        geo (float)
        hardware_recovery (float)
        total (float)
        user_traffic (float)
        xor (float)
```
(出典: ECS_DOC:1300-1310, OS_DOC:1330-1340)

両マニュアルは完全に一致する。
database は `monitoring_vdc` である。

### 2.7 ノードシステムレベルデータ（CPU/メモリ）の詳細対応

`nodeCpuUtilization*` / `nodeMemoryUtilization*` の代替は、「メトリクスの監視リスト」の一般カタログでは database/measurement 名（`monitoring_op` / `cpu`, `mem`, `net`）までしか示されない。
一方、ECS_DOC の「非推奨 Dashboard API に対する Flux API の代替」節（OS_DOC には存在しない、5章参照）では、具体的なフィールドと Flux クエリ例まで示されている。

- `nodeCpuUtilization` 相当：`monitoring_op` / measurement `cpu` / field `usage_idle`（アイドル CPU 使用率、パーセント）。tag: `host`（ecs_node_fqdn）, `node_id`, `range`（最大1時間）。
  ```flux
  from(bucket: "monitoring_op")
  |> filter(fn: (r) => r._measurement == "cpu" and r.cpu == "cpu-total" and r._field ==
  "usage_idle" and r.host == "ecs_node_fqdn")
  |> range(start: -1h)
  |> keep(columns: ["_time", "_value", "host"])
  ```
  `usage_idle` はアイドル率であり utilization（使用率）ではない。
  utilization を算出するには `100 - usage_idle` 等の計算が必要と考えられるが、この計算式自体はマニュアルに明記されていない（**記載なし**）。
  (出典: ECS_DOC:1808-1857)

- `nodeMemoryUtilization` 相当：`monitoring_op` / measurement `mem` / field `free`（ホスト上の空きメモリ、バイト）。tag: `host`, `node_id`, `range`（最大1時間）。
  ```flux
  from(bucket: "monitoring_op")
  |> filter(fn: (r) => r._measurement == "mem" and r._field == "free" and r.host ==
  "ecs_node_fqdn")
  |> range(start: -1h)
  |> keep(columns: ["_time", "_value", "host"])
  ```
  `mem` measurement には `used_percent` / `available_percent` という比率フィールドも存在する（3章参照）。
  マニュアルの本節は `free`（絶対値）を例示しているのみで、`nodeMemoryUtilization`（率）との対応が `used_percent` なのか `free` から計算するのかは明言されていない（**記載なし**、`free` を使う例のみが明記）。
  (出典: ECS_DOC:1859-1896)

この節（プロセス統計、ノード統計の Flux API 代替、Flux クエリ例、`range` 上限1時間の明記）は ECS_DOC:1713-1907 にのみ存在し、OS_DOC には対応する記述がない。

---

## 3. `monitoring_vdc` の `cq_*` measurement 一覧

`monitoring_vdc` データベースの値は特定のデータノードを参照せず、VDC 全体で計算される。
(出典: ECS_DOC:1296, OS_DOC:1322)

両マニュアルとも database 名は `monitoring_vdc` で同一である（`monitoring_vdc_os` のような ObjectScale 専用のサフィックス付き名称は存在しない。grep でも確認済み）。

### 3.1 非パフォーマンス系（「メトリクスの監視リスト: 非パフォーマンス」節）

```text
Measurement: cq_disk_bandwidth
Tags: type_op ('read', 'write')
Fields: consistency_checker (float)
        erasure_encoding (float)
        geo (float)
        hardware_recovery (float)
        total (float)
        user_traffic (float)
        xor (float)
```
(出典: ECS_DOC:1300-1310, OS_DOC:1330-1340。完全一致)

```text
Measurement: cq_node_rebalancing_summary
Tags: none
Fields: data_rebalanced (integer)
        pending_rebalance (integer)   -- ECS_DOCのみ記載。OS_DOCにはこのフィールドの記載がない（下記差分参照）
```
(出典: ECS_DOC:1313-1317, OS_DOC:1343-1346)

> **ECS版とOS版の差分**：ECS_DOC は `cq_node_rebalancing_summary` に `data_rebalanced` と `pending_rebalance` の2フィールドを記載する（ECS_DOC:1315-1316）。
> OS_DOC は同じ measurement 定義ブロックに `data_rebalanced` の1フィールドのみを記載しており、`pending_rebalance` の記載がない（OS_DOC:1343-1346）。
> ドキュメント上の省略の可能性があり、実機でのフィールド存在有無は未確認である（**記載なし**＝OS側での `pending_rebalance` 有無は本文からは判断不能）。

```text
Measurement: cq_process_health
Tags: none
Fields: cpu_used (float)
        mem_used (float)
        mem_used_percent (float)
        nic_bytes (float)
        nic_utilization (float)
```
(出典: ECS_DOC:1320-1327, OS_DOC:1349-1356。完全一致)

```text
Measurement: cq_recover_status_summary
Tags: none
Fields: data_recovered (integer)
        data_to_recover (integer)
```
(出典: ECS_DOC:1330-1334, OS_DOC:1359-1363。完全一致)

### 3.2 パフォーマンス系（「メトリクスの監視リスト: パフォーマンス」節）

値の性質に関する注記は、両マニュアル共通で次のとおりである。

- `_delta` で終わらない measurement：レート（毎秒のリクエスト数）
- `_delta` で終わる measurement：デルタ値（前のタイムスタンプからのカウンターの増分）
- `_downsampled` で終わる measurement：ダウンサンプリング値（1日1ポイントに集約）

(出典: ECS_DOC:1457-1461, OS_DOC:1488-1492)

以下、ECS_DOC:1463-1712 / OS_DOC:1494-1741 に記載の全 `cq_performance_*` measurement を列挙する（両マニュアル完全一致）。

| Measurement | Tags | Fields |
|---|---|---|
| `cq_performance_error` | none | `system_errors` (float), `user_errors` (float) |
| `cq_performance_error_downsampled` | none | 同上 |
| `cq_performance_error_code` | `code` | `error_counter` (float) |
| `cq_performance_error_code_downsampled` | `code` | 同上 |
| `cq_performance_error_delta` | none | `system_errors_i` (integer), `user_errors_i` (integer) |
| `cq_performance_error_delta_downsampled` | none | 同上 |
| `cq_performance_error_head` | `head` | `system_errors` (float), `user_errors` (float) |
| `cq_performance_error_head_downsampled` | `head` | 同上 |
| `cq_performance_error_head_delta` | `head` | `system_errors_i` (integer), `user_errors_i` (integer) |
| `cq_performance_error_head_delta_downsampled` | `head` | 同上 |
| `cq_performance_error_ns` | `namespace` | `system_errors` (float), `user_errors` (float) |
| `cq_performance_error_ns_downsampled` | `namespace` | 同上 |
| `cq_performance_error_ns_delta` | `namespace` | `system_errors_i` (integer), `user_errors_i` (integer) |
| `cq_performance_error_ns_delta_downsampled` | `namespace` | 同上 |
| `cq_performance_latency` | `id` | `p50` (float), `p99` (float) |
| `cq_performance_latency_downsampled` | `id` | 同上 |
| `cq_performance_latency_head` | `head`, `id` | `p50` (float), `p99` (float) |
| `cq_performance_latency_head_downsampled` | `head`, `id` | 同上 |
| `cq_performance_throughput` | none | `total_read_requests_size` (float), `total_write_requests_size` (float) |
| `cq_performance_throughput_downsampled` | none | 同上 |
| `cq_performance_throughput_head` | `head` | `total_read_requests_size` (float), `total_write_requests_size` (float) |
| `cq_performance_throughput_head_downsampled` | `head` | 同上 |
| `cq_performance_transaction` | none | `failed_request_counter` (float), `succeed_request_counter` (float) |
| `cq_performance_transaction_downsampled` | none | 同上 |
| `cq_performance_transaction_delta` | none | `failed_request_counter_i` (integer), `succeed_request_counter_i` (integer) |
| `cq_performance_transaction_delta_downsampled` | none | 同上 |
| `cq_performance_transaction_head` | `head` | `failed_request_counter` (float), `succeed_request_counter` (float) |
| `cq_performance_transaction_head_downsampled` | `head` | 同上 |
| `cq_performance_transaction_head_delta` | `head` | `failed_request_counter_i` (integer), `succeed_request_counter_i` (integer) |
| `cq_performance_transaction_head_delta_downsampled` | `head` | 同上 |
| `cq_performance_transaction_method` | `method` | `failed_request_counter` (float), `succeed_request_counter` (float) |
| `cq_performance_transaction_method_downsampled` | `method` | 同上 |
| `cq_performance_transaction_ns` | `namespace` | `failed_request_counter` (float), `succeed_request_counter` (float) |
| `cq_performance_transaction_ns_downsampled` | `namespace` | 同上 |
| `cq_performance_transaction_ns_delta` | `namespace` | `failed_request_counter_i` (integer), `succeed_request_counter_i` (integer) |
| `cq_performance_transaction_ns_delta_downsampled` | `namespace` | 同上 |

(出典: ECS_DOC:1463-1712, OS_DOC:1494-1741)

---

## 4. `monitoring_op` の procstat / cpu / mem measurement

`monitoring_op` の「ノードシステムレベル統計」節の measurement は、デフォルトの Telegraf プラグインに由来し、measurement 名はプラグイン名と同一である。
(出典: ECS_DOC:995-1002, OS_DOC:1018-1029)

### 4.1 `cpu`

```text
Measurement: cpu
Tags: cpu, host, node_id, tag
Fields: usage_guest (float)
        usage_guest_nice (float)
        usage_idle (float)
        usage_iowait (float)
        usage_irq (float)
        usage_nice (float)
        usage_softirq (float)
        usage_steal (float)
        usage_system (float)
        usage_user (float)
```
(出典: ECS_DOC:1004-1017, OS_DOC:1030-1043。完全一致)

### 4.2 `mem`

```text
Measurement: mem
Tags: host, node_id, tag
Fields: active (integer)
        available (integer)
        available_percent (float)
        buffered (integer)
        cached (integer)
        commit_limit (integer)
        committed_as (integer)
        dirty (integer)
        free (integer)
        high_free (integer)
        high_total (integer)
        huge_page_size (integer)
        huge_pages_free (integer)
        huge_pages_total (integer)
        inactive (integer)
        low_free (integer)
        low_total (integer)
        mapped (integer)
        page_tables (integer)
        shared (integer)
        slab (integer)
        swap_cached (integer)
        swap_free (integer)
        swap_total (integer)
        total (integer)
        used (integer)
        used_percent (float)
        vmalloc_chunk (integer)
        vmalloc_total (integer)
        vmalloc_used (integer)
        wired (integer)
        write_back (integer)
        write_back_tmp (integer)
```
(出典: ECS_DOC:1062-1098, OS_DOC:1088-1124。完全一致)

### 4.3 `procstat`

```text
Measurement: procstat
Tags: host, node_id, process_name, tag, user
Fields: cpu_time (integer)
        cpu_time_guest (float)
        cpu_time_guest_nice (float)
        cpu_time_idle (float)
        cpu_time_iowait (float)
        cpu_time_irq (float)
        cpu_time_nice (float)
        cpu_time_soft_irq (float)
        cpu_time_steal (float)
        cpu_time_stolen (float)
        cpu_time_system (float)
        cpu_time_user (float)
        cpu_usage (float)
        create_time (integer)
        involuntary_context_switches (integer)
        memory_data (integer)
        memory_locked (integer)
        memory_rss (integer)
        memory_stack (integer)
        memory_swap (integer)
        memory_vms (integer)
        nice_priority (integer)
        num_fds (integer)
        num_threads (integer)
        pid (integer)
        read_bytes (integer)
        read_count (integer)
        realtime_priority (integer)
        rlimit_cpu_time_hard (integer)
        rlimit_cpu_time_soft (integer)
        rlimit_file_locks_hard (integer)
        rlimit_file_locks_soft (integer)
        rlimit_memory_data_hard (integer)
        rlimit_memory_data_soft (integer)
        rlimit_memory_locked_hard (integer)
        rlimit_memory_locked_soft (integer)
        rlimit_memory_rss_hard (integer)
        rlimit_memory_rss_soft (integer)
        rlimit_memory_stack_hard (integer)
        rlimit_memory_stack_soft (integer)
        rlimit_memory_vms_hard (integer)
        rlimit_memory_vms_soft (integer)
        rlimit_nice_priority_hard (integer)
        rlimit_nice_priority_soft (integer)
        rlimit_num_fds_hard (integer)
        rlimit_num_fds_soft (integer)
        rlimit_realtime_priority_hard (integer)
        rlimit_realtime_priority_soft (integer)
        rlimit_signals_pending_hard (integer)
        rlimit_signals_pending_soft (integer)
        signals_pending (integer)
        voluntary_context_switches (integer)
        write_bytes (integer)
        write_count (integer)
```
(出典: ECS_DOC:1141-1198, OS_DOC:1167-1224。完全一致)

`procstat` の詳細なフィールド、タグの説明については、両マニュアルとも「GitHub Influx Data Telegraf Inputs Procstat」を参照するよう案内している（社外ドキュメントへの参照であり本文には全項目の説明は書かれていない）。
(出典: ECS_DOC:1732, OS_DOC には同等の言及なし)

### 4.4 `process_name` タグの値一覧（ECS_DOCのみ）

`procstat` の `process_name` タグが取りうる有効なプロセス名の一覧は、ECS_DOC の「非推奨 Dashboard API に対する Flux API の代替 > プロセス統計」節に記載されている。
OS_DOC にはこの一覧に相当する記述が存在しない。

```text
nvmeengine
nvmetargetviewer
dtsm
rack-service-manager
rpcbind
blobsvc
cm
coordinatorsvc
dataheadsvc
dtquery
ecsportalsvc
eventsvc
georeceiver
metering
objcontrolsvc
resourcesvc
transformsvc
vnest
fluxd
influxd
throttler
grafana-server
dockerd
fabric-agent
fabric-lifecycle
fabric-registry
fabric-zookeeper
```
(出典: ECS_DOC:1742-1769)

同節にはプロセス統計取得の Flux クエリ例も示されている。

```flux
from(bucket: "monitoring_op")
|> filter(fn: (r) => r._measurement == "procstat" and r._field == "memory_rss" and
r.process_name == "vnest" and r.host == "ecs_node_fqdn")
|> range(start: -1h)
|> keep(columns: ["_time", "_value", "process_name"])
```

出力例（CSV）は、次のとおりである。

```csv
#datatype,string,long,dateTime:RFC3339,long,string
#group,false,false,false,false,true
#default,_result,,,,
,result,table,_time,_value,process_name
,,0,2019-08-15T13:05:00Z,2505809920,vnest
,,0,2019-08-15T13:10:00Z,2505887744,vnest
,,0,2019-08-15T13:15:00Z,2506014720,vnest
,,0,2019-08-15T13:20:01Z,2506010624,vnest
```
(出典: ECS_DOC:1787-1806)

ここで `/dashboard/processes/{id}` の代替として使う場合、`{id}` は `<node_id>-<process_name>` の形式（例: `330e4b8f-4491-4ec7-b816-7b10ac9c6abf-cm`）で構成され、`r.node_id` と `r.process_name` にそれぞれ分解してフィルタする、との説明がある。
(出典: ECS_DOC:1776-1783)

`procstat` の Dashboard API 代替として明記されているフィールドは、以下の3つのみである（`procstat` 自体は40以上のフィールドを持つが、Dashboard API 相当として使うのはこの3つ）。

- `memory_rss`：プロセスの常駐メモリ（バイト）
- `cpu_usage`：プロセスの CPU 使用率（単一 CPU に対する使用率）
- `num_threads`：プロセスが使用するスレッド数（int）

(出典: ECS_DOC:1734-1738)

---

## 5. 制約事項

### 5.1 `range` の上限（ECS_DOCのみ明記）

「非推奨 Dashboard API に対する Flux API の代替」節（プロセス統計、ノード統計 `cpu`、ノード統計 `mem` の3箇所）で、いずれも以下の記載がある。

> リソース制限により、`range` は最大1時間に制限されている。

(出典: ECS_DOC:1772-1774, 1832-1834, 1871-1873)

OS_DOC にはこの制約に関する記述がない（対応する節自体が存在しないため）。
ObjectScale で同じ制限が適用されるかどうかは**記載なし**である。

### 5.2 データ保持期間（retention）

両マニュアルとも「Process Health - Process List by Node」ダッシュボードの「Process Restarts」フィールドの説明において以下の記載がある。

> 選択した時間範囲内でノード上のプロセスが最後に再起動した時刻。最大時間範囲は保持ポリシーにより5日に制限される場合がある。

(出典: ECS_DOC:97, OS_DOC:93)

これ以外に、`monitoring_main` / `monitoring_last` / `monitoring_op` / `monitoring_vdc` 各データベースの一般的なデータ保持期間（何日分のデータを保持するか）について、両マニュアルとも明記していない。
**記載なし**（5日という数値は Process Restarts の UI 上の最大表示範囲についての言及であり、DB 自体の retention policy の日数として直接保証されたものではない）。

### 5.3 集計間隔とダウンサンプリング

- `_downsampled` サフィックスを持つ measurement は「1日1ポイントに集約」されたダウンサンプリング値である。(出典: ECS_DOC:1461, OS_DOC:1492)
- `_delta` サフィックスを持たない measurement はレート（毎秒のリクエスト数）、`_delta` サフィックスを持つ measurement は前のタイムスタンプからのカウンター増分（デルタ値）である。(出典: ECS_DOC:1457-1460, OS_DOC:1488-1491)
- 自動メータリング再構築の節では、メータリング統計は「最も近い5分の倍数の時刻」に集約、マッピングされる、との記載がある（例: 10:04:59 pm に作成されたオブジェクトは 10:00:00 pm にマッピングされる）。
  ただしこれはメータリング（バケット利用量）統計に関する記述であり、Flux API の performance/非performance measurement 全般の集計間隔を示すものではない。(出典: ECS_DOC:339-348, OS_DOC:395-404)
- OS_DOC のみ、「Capacity Utilization」ダッシュボードのデータは60分ごとに更新される、との記載がある。(出典: OS_DOC:21)
  これは Capacity Utilization 固有の更新間隔であり、ECS_DOC にはこのダッシュボード自体が表93/表25相当の一覧に存在しない（6章参照）。

**記載なし**：`monitoring_main` / `monitoring_op` の生データ（非 `_downsampled` / 非 `_delta`）の収集間隔（サンプリング周期）が何秒、何分か。
データポイントの書き込み頻度。

---

## 6. ECS版とObjectScale版の差分まとめ

### 6.1 同一の項目

| 項目 | 内容 |
|---|---|
| Flux API エンドポイント URL | `https://localhost:4443/flux/api/external/v2/query`。同一。 |
| HTTP メソッド | `POST`。同一。 |
| 認証トークン取得エンドポイント | `https://localhost:4443/login`（`-u emcmonitor:####`、`X-SDS-AUTH-TOKEN` ヘッダ取得）。同一（ただし OS_DOC でも `admin@ecs:` プロンプトのままであり、記述が ECS 由来のまま流用されている可能性がある）。 |
| 必要ロール | `SYSTEM_ADMIN` または `SYSTEM_MONITOR`。同一。 |
| リクエスト/レスポンス形式（JSON、CSV） | ヘッダ、ボディ形式とも同一。 |
| database（bucket）名 | `monitoring_main`, `monitoring_last`, `monitoring_op`, `monitoring_vdc`。すべて同一の名称（`monitoring_vdc_os` のようなサフィックス付き別名は存在しない）。 |
| `monitoring_op` の `cpu`/`mem`/`procstat` measurement 定義 | tag、field とも完全に同一。 |
| `monitoring_vdc` の `cq_performance_*`（パフォーマンス系）measurement 定義 | すべて完全に同一。 |
| `monitoring_vdc` の `cq_disk_bandwidth` / `cq_process_health` / `cq_recover_status_summary` | 完全に同一。 |
| ダウンサンプリング、デルタ、レートの説明文 | ほぼ同一の文言。 |
| Process Restarts の5日保持ポリシー言及 | 同一。 |

### 6.2 差分がある項目

| 項目 | ECS_DOC | OS_DOC |
|---|---|---|
| 削除フィールド対応表（表27相当） | 「表27. 削除されたデータの代替場所」として存在（ECS_DOC:1948-1969） | 記載なし。表番号、章立て自体が存在しない |
| 削除された API 一覧（表28相当） | 「表28. ECS 3.5.0 で削除された API」として存在（ECS_DOC:1975-1980） | 記載なし |
| 「Dashboard APIs」章（ECS 3.6.0.0 での変更 API 一覧、ECS 3.5.0.0 での削除 API 一覧） | 存在（ECS_DOC:1908-1980） | 記載なし |
| 「非推奨 Dashboard API に対する Flux API の代替」節（プロセス統計、ノード統計 `cpu`、ノード統計 `mem` の具体的な Flux クエリ例、field 対応） | 存在（ECS_DOC:1713-1907） | 記載なし |
| `procstat` の `process_name` タグの値一覧（27種類のプロセス名） | 存在（ECS_DOC:1742-1769） | 記載なし |
| `range` 上限1時間の明記 | 存在（3箇所） | 記載なし |
| `cq_node_rebalancing_summary` のフィールド | `data_rebalanced`, `pending_rebalance` の2フィールド | `data_rebalanced` の1フィールドのみ（`pending_rebalance` の記載なし） |
| Advanced Monitoring ダッシュボード一覧（表25 vs 表93） | 「Node Rebalancing」の単一ダッシュボード。「Capacity Utilization」「Compression Overview」「Data Movement - Source/Target Information」のダッシュボードは一覧に存在しない | 「Node Rebalance - Node Rebalance Progress」ダッシュボードが「Node Rebalance Status - by Node」テーブルと「Node Rebalance History by SS」パネル群に分かれて構成される。加えて ECS_DOC にはない「Capacity Utilization」「Compression Overview」「Data Movement - Source/Target Information」ダッシュボードが存在する |
| Data Access Performance ダッシュボードの精度注記 | 記載なし | 「大きな時間範囲では正確でない場合がある。ズームインするか、比較的短い時間範囲を選択すること」という注記あり（OS_DOC:23） |
| Capacity Utilization ダッシュボードの更新間隔（60分ごと） | 該当ダッシュボードが表に存在しないため言及なし | 記載あり（OS_DOC:21） |

### 6.3 exporter 改修設計への含意

本改修の直接の動機である「表27相当の対応表」「procstat process_name 一覧」「range 上限」は、いずれも ECS_DOC のみに明記された情報であり、ObjectScale 4.x 側のマニュアルには同等の記述が存在しない。
ObjectScale 版 exporter を実装する際、これらの制約（`range` 上限1時間、`process_name` の有効値、削除フィールドの具体的な代替 measurement）が ECS と同一かどうかはマニュアルからは検証できない。
実機での確認、または Dell サポートや KB 記事の追加調査が必要である。

---

## 付録

### A. 本書で参照した主な行範囲の出典一覧

**ECS_DOC**（`/home/nagata_yasuhiro/miyamado/user-manuals/dell-ecs/monitoring-guide/04-advanced-monitoring.md`）

- 368-421: Flux API 概要、前提条件、リクエストペイロード例
- 422-531: curl 実行例（JSON/CSV）
- 532-538: 共通タグ
- 540-563: `cq_gc_data` / `cq_gc_remaining_elements` フィールド説明
- 564-992: `monitoring_main` / `monitoring_last`（非パフォーマンス）measurement カタログ
- 993-1293: `monitoring_op` measurement カタログ（`cpu`, `disk`, `diskio`, `mem`, `net`, `nstat`, `processes`, `procstat`, `swap`, `system`, DT統計, Fabric agent 統計, SR journal 統計, Vnest Btree 統計）
- 1294-1335: `monitoring_vdc`（非パフォーマンス）`cq_*` measurement
- 1336-1712: `monitoring_main` / `monitoring_vdc`（パフォーマンス）measurement カタログ
- 1713-1907: 「非推奨 Dashboard API に対する Flux API の代替」（プロセス統計、ノード統計）
- 1908-1980: 「Dashboard APIs」章（表27、表28）

**OS_DOC**（`/home/nagata_yasuhiro/miyamado/user-manuals/dell-objectscale/admin-guide/15-advanced-monitoring.md`）

- 421-473: Flux API 概要、前提条件、リクエストペイロード例
- 474-545: curl 実行例（JSON/CSV）
- 547-553: 共通タグ
- 555-577: `cq_gc_data` / `cq_gc_remaining_elements` フィールド説明
- 579-1017: `monitoring_main` / `monitoring_last`（非パフォーマンス）measurement カタログ
- 1018-1319: `monitoring_op` measurement カタログ
- 1320-1364: `monitoring_vdc`（非パフォーマンス）`cq_*` measurement
- 1365-1741: `monitoring_main` / `monitoring_vdc`（パフォーマンス）measurement カタログ
- （1742行で終了。「非推奨 Dashboard API に対する Flux API の代替」「Dashboard APIs」章に相当する記述なし）

### B. 本書全体の「記載なし」まとめ（未確認事項リスト）

1. Flux API トークンの有効期限、リフレッシュ方法。
2. Flux API の実際の到達可能エンドポイント（管理ノード外部からの呼び出し方法）。
3. `usage_idle`（CPU）や `free`（メモリ）から `nodeCpuUtilization`/`nodeMemoryUtilization` 相当の使用率(%)を算出する具体的な計算式。
4. `transactionReadLatency`/`transactionWriteLatency` について、read/write を区別する具体的なタグ、クエリ方法。`cq_performance_latency`/`statDataHead_performance_internal_latency` の `id` タグが取りうる値の一覧。
5. `monitoring_main`/`monitoring_op`/`monitoring_vdc`/`monitoring_last` 各データベースの一般的なデータ保持期間（日数）。
6. `monitoring_main`/`monitoring_op` の生データ収集間隔（サンプリング周期）。
7. ObjectScale 4.x における `range` 上限1時間の制約の有無。
8. ObjectScale 4.x における `procstat` の `process_name` タグの有効値一覧。
9. `cq_node_rebalancing_summary` の `pending_rebalance` フィールドが ObjectScale 4.x でも存在するか。
10. ECS_DOC の「非推奨 Dashboard API に対する Flux API の代替」節および表27、表28に相当する情報が、ObjectScale 4.x の別マニュアル（本調査で対象としていない文書）に存在するかどうか。
</content>
