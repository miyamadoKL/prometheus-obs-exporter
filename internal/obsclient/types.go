// Package obsclient は Dell ECS 3.8.x / ObjectScale 4.x の management,
// metering, Flux（時系列）REST API 向けの HTTP クライアントを実装する。
//
// このファイルは契約型を定義する: それらの API が返す JSON（DT-stats/ping
// エンドポイントについては XML）レスポンス body の型付き表現。コレクター
// コード（internal/collector）は、これらの型と dashboard.go / metering.go /
// dtstats.go / flux.go のクライアントメソッドのみに依存すべきで、生の JSON
// に依存してはならない。
package obsclient

import (
	"errors"
	"strconv"
	"strings"
)

// ErrMissing は、元の JSON フィールドが存在しないか null だった場合
// （空文字列としてデコードされる）に FlexNumber.Float64 / FlexNumber.Int64
// が返す。呼び出し側（コレクターコード）はこれを他のパースエラーと同様に
// 扱う: 既にある "err == nil" ガードにより、フィールドが欠けている場合は
// 単にそのメトリクスシリーズを出力しないだけで、誤解を招く偽の0を
// 出力することはない。
var ErrMissing = errors.New("obsclient: value missing")

// FlexNumber は、ECS / ObjectScale の dashboard/metering API がネイティブな
// JSON 数値（例: 123, 1.5）としても、クォート付き JSON 文字列（例: "123",
// "1.5"）としても表現しうる数値をデコードする - 同じフィールドで両方の
// 形式が API バージョンをまたいで観測されている。リテラルなテキストを
// そのまま保持し、Float64/Int64 アクセサを公開する。encoding/json.Number に
// 似ているが、クォート付き形式も許容する点が異なる。
type FlexNumber string

// UnmarshalJSON は json.Unmarshaler を実装する。素の JSON 数値トークンと、
// 数値を含む JSON 文字列の両方を受け付ける。
func (n *FlexNumber) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" {
		*n = ""
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	*n = FlexNumber(s)
	return nil
}

// Float64 は数値を float64 としてパースする。空の値（フィールド欠落または
// null）の場合は、偽の0ではなく (0, ErrMissing) を返す。
func (n FlexNumber) Float64() (float64, error) {
	if n == "" {
		return 0, ErrMissing
	}
	return strconv.ParseFloat(string(n), 64)
}

// Int64 は数値を int64 としてパースする。空の値（フィールド欠落または
// null）の場合は、偽の0ではなく (0, ErrMissing) を返す。
func (n FlexNumber) Int64() (int64, error) {
	if n == "" {
		return 0, ErrMissing
	}
	return strconv.ParseInt(string(n), 10, 64)
}

func (n FlexNumber) String() string { return string(n) }

// ---------------------------------------------------------------------
// Auth: GET /login, GET /user/whoami, GET /logout
// ---------------------------------------------------------------------

// WhoAmI は GET /user/whoami のレスポンス body で、キャッシュされた認証
// トークンがまだ使えるか検証するのに使う。
type WhoAmI struct {
	CommonName string   `json:"common_name"`
	Roles      []string `json:"roles"`
}

// ---------------------------------------------------------------------
// Dashboard API: GET /dashboard/zones/localzone
// ---------------------------------------------------------------------

// CountSample は dashboard API の "Num*" 系シリーズの1つの過去サンプル。
// 例えば alertsNumUnackCritical の1要素。
type CountSample struct {
	Count FlexNumber `json:"Count"`
}

// SpaceSample は dashboard API のディスク容量シリーズの1つの過去サンプル。
// 例えば diskSpaceTotalCurrent の1要素。単位は docs/design.md によれば GB。
// バイトへの変換はコレクター層の責務。
type SpaceSample struct {
	Space FlexNumber `json:"Space"`
}

// FirstCount は Count シリーズの最初（現在）のサンプルの Count を返す。
// シリーズが空なら (0, false) を返す。
func FirstCount(samples []CountSample) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	v, err := samples[0].Count.Float64()
	if err != nil {
		return 0, false
	}
	return v, true
}

// FirstSpace は Space シリーズの最初（現在）のサンプルの Space を返す。
// シリーズが空なら (0, false) を返す。
func FirstSpace(samples []SpaceSample) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	v, err := samples[0].Space.Float64()
	if err != nil {
		return 0, false
	}
	return v, true
}

// LocalZone は GET /dashboard/zones/localzone のレスポンス body。
// ECS 3.6.0.0+ では旧バージョンで文書化されていた transaction* /
// diskRead(Write)BandwidthTotal / nodeCpuUtilization フィールドが
// 削除されている。docs/design.md によればまだ存在するフィールドのみを
// ここでモデル化している。
type LocalZone struct {
	Name string `json:"name"`

	NumBadDisks  FlexNumber `json:"numBadDisks"`
	NumBadNodes  FlexNumber `json:"numBadNodes"`
	NumGoodNodes FlexNumber `json:"numGoodNodes"`
	NumGoodDisks FlexNumber `json:"numGoodDisks"`

	AlertsNumUnackCritical []CountSample `json:"alertsNumUnackCritical"`
	AlertsNumUnackError    []CountSample `json:"alertsNumUnackError"`
	AlertsNumUnackInfo     []CountSample `json:"alertsNumUnackInfo"`
	AlertsNumUnackWarning  []CountSample `json:"alertsNumUnackWarning"`

	DiskSpaceFreeCurrent      []SpaceSample `json:"diskSpaceFreeCurrent"`
	DiskSpaceTotalCurrent     []SpaceSample `json:"diskSpaceTotalCurrent"`
	DiskSpaceAllocatedCurrent []SpaceSample `json:"diskSpaceAllocatedCurrent"`
}

// ---------------------------------------------------------------------
// Dashboard API: GET /dashboard/zones/localzone/replicationgroups
// ---------------------------------------------------------------------

// ReplicationGroup は GET /dashboard/zones/localzone/replicationgroups が
// 返す replicationgroup 配列の1エントリ。旧 exporter はこのデータを "node"
// と誤ってラベル付けしていた。docs/design.md ではコレクターラベルを
// "rg"（replication group 名）に改名している。
type ReplicationGroup struct {
	Name string `json:"name"`

	ReplicationIngressTraffic                FlexNumber `json:"replicationIngressTraffic"`
	ReplicationEgressTraffic                 FlexNumber `json:"replicationEgressTraffic"`
	ChunksRepoPendingReplicationTotalSize    FlexNumber `json:"chunksRepoPendingReplicationTotalSize"`
	ChunksJournalPendingReplicationTotalSize FlexNumber `json:"chunksJournalPendingReplicationTotalSize"`
	ChunksPendingXorTotalSize                FlexNumber `json:"chunksPendingXorTotalSize"`
	// ReplicationRpoTimestamp は epoch タイムスタンプ。docs/design.md は
	// コレクター層で epoch 秒への変換が必要だと注記している
	// （dashboard API は epoch ミリ秒を返すことが観測されている）。
	ReplicationRpoTimestamp FlexNumber `json:"replicationRpoTimestamp"`
}

// ReplicationGroupsResponse は
// GET /dashboard/zones/localzone/replicationgroups のトップレベルの
// レスポンス body。
type ReplicationGroupsResponse struct {
	Replicationgroup []ReplicationGroup `json:"replicationgroup"`
}

// ---------------------------------------------------------------------
// Dashboard API: GET /vdc/nodes
// ---------------------------------------------------------------------

// Node は GET /vdc/nodes が返す node 配列の1エントリ。
type Node struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	DataIP  string `json:"data_ip"`
	MgmtIP  string `json:"mgmt_ip"`
	Version string `json:"version"`
}

// NodesResponse は GET /vdc/nodes のトップレベルのレスポンス body。
type NodesResponse struct {
	Node []Node `json:"node"`
}

// ---------------------------------------------------------------------
// Metering API: /object/namespaces, /object/namespaces/namespace/{ns}/quota,
// /object/billing/namespace/{ns}/info
// ---------------------------------------------------------------------

// NamespaceRef は GET /object/namespaces が返す namespace 配列の1エントリ。
type NamespaceRef struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// NamespacesResponse は GET /object/namespaces のトップレベルの
// レスポンス body。
type NamespacesResponse struct {
	Namespace []NamespaceRef `json:"namespace"`
}

// NamespaceQuota は
// GET /object/namespaces/namespace/{namespace}/quota のレスポンス body。
// docs/design.md によればサイズは GB 単位。バイトへの変換はコレクター層の
// 責務。
type NamespaceQuota struct {
	Namespace        string     `json:"namespace"`
	BlockSize        FlexNumber `json:"blockSize"`
	NotificationSize FlexNumber `json:"notificationSize"`
}

// NamespaceBillingInfo は
// GET /object/billing/namespace/{namespace}/info のレスポンス body。
// total_size は sizeunit クエリパラメータで要求した単位（obsclient は常に
// KB を要求する）。バイトへの変換はコレクター層の責務。
type NamespaceBillingInfo struct {
	Namespace    string     `json:"namespace"`
	TotalObjects FlexNumber `json:"total_objects"`
	TotalSize    FlexNumber `json:"total_size"`
}

// ---------------------------------------------------------------------
// DT statistics: http://<node>:9101/stats/dt/DTInitStat (XML)
// ---------------------------------------------------------------------

// DTInitStat は http://<node>:9101/stats/dt/DTInitStat をパースした body。
// ルート要素名は文書化されていないため、<entry> 子要素とその子孫のみに
// 依存している（旧 ecsclient.go の挙動に合わせている）。
type DTInitStat struct {
	TotalDTNum   float64 `xml:"entry>total_dt_num"`
	UnreadyDTNum float64 `xml:"entry>unready_dt_num"`
	UnknownDTNum float64 `xml:"entry>unknown_dt_num"`
}

// ---------------------------------------------------------------------
// Ping / active connections: https://<node>:<objPort>/?ping (XML)
// ---------------------------------------------------------------------

// PingResponse は https://<node>:<objPort>/?ping をパースした body。
type PingResponse struct {
	Xmlns  string   `xml:"xmlns,attr"`
	Name   []string `xml:"PingItem>Name"`
	Value  float64  `xml:"PingItem>Value"`
	Status []string `xml:"PingItem>Status"`
	Text   []string `xml:"PingItem>Text"`
}

// ---------------------------------------------------------------------
// Flux API: POST /flux/api/external/v2/query
// ---------------------------------------------------------------------

// fluxQueryRequest は Flux API へ送る JSON リクエスト body。
type fluxQueryRequest struct {
	Query string `json:"query"`
}

// FluxSeries は Flux API の JSON レスポンスにおける "Series" 配列の
// 1要素。Values はデータポイントごとに1行を含み、各セルは文字列
// （docs/api-research/flux-api-reference.md によれば、Flux の JSON 値は
// Datatypes に関わらず全て文字列表現）。
type FluxSeries struct {
	Datatypes []string   `json:"Datatypes"`
	Columns   []string   `json:"Columns"`
	Values    [][]string `json:"Values"`
}

// FluxQueryResponse は POST /flux/api/external/v2/query のトップレベルの
// JSON レスポンス body。
type FluxQueryResponse struct {
	Series []FluxSeries `json:"Series"`
}
