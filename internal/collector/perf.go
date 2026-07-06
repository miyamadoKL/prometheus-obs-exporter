// このファイルは Registry["perf"] を実装する: Flux 時系列 API
// （POST /flux/api/external/v2/query に対する obsclient.Client.Query）から
// 得られる VDC 集計パフォーマンスメトリクス。docs/design.md の perf
// コレクター契約テーブルに基づく。
//
// 全ての Flux クエリ定義（measurement 名、field 名、tag 名/値、クエリ構築
// ロジック）は、この直後の定数/テーブルブロックにまとめてある。これは
// パース/メトリクス出力ロジックと意図的に分離しており、実機クラスタでの
// 検証によりこれらの選択が確認・修正された際にはこのブロックだけを
// 変更すれば済むようにするため。
package collector

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------
// Flux クエリ定義 - measurement / field / tag 名とクエリ構築。
// 出典は docs/api-research/flux-api-reference.md。各選択の横に該当する
// セクション番号を記載している。
// ---------------------------------------------------------------------

// perfBucket はこのファイルの全メトリクスで問い合わせる Flux バケット。
// ECS と ObjectScale の両方で同一であることを確認済み
// （flux-api-reference.md section 6.1）。
const perfBucket = "monitoring_vdc"

// 以下の各 Flux クエリで使われる遡及期間（常に |> last() と組み合わせて
// その期間内の最新値のみ取得する。docs/design.md: "range(...) + last() で
// 最新値のみ取得する"）は run.Settings.PerfRange から来る
// （docs/design.md: --collector.perf.range / OBS_EXPORTER_PERF_RANGE、
// デフォルト 5m - internal/config/config.go の CollectorPerfRange 参照）。
//
// 注: Flux API のレンジ上限は 1h とされているが、これは ECS マニュアルにのみ
// 記載されており、flux-api-reference.md section 5.1 では ObjectScale では
// この制約が未確認であると注記している。internal/config/config.go は
// 1h を超える設定を起動時に拒否する。

const (
	// スループット: flux-api-reference.md section 3.2 (cq_performance_*
	// カタログ) と section 2.4 の信頼度テーブル (信頼度: 高 - field 名が
	// read/write で明確に分かれている)。
	measThroughput  = "cq_performance_throughput"
	fieldReadBytes  = "total_read_requests_size"
	fieldWriteBytes = "total_write_requests_size"

	// TPS: flux-api-reference.md section 2.4 (信頼度: 中 - READ/WRITE の
	// tag 値から read/write-TPS へのマッピングは、method tag の文書化された
	// 値からの推測であり、どちらのマニュアルにも明示されていない)。
	measTransactionMethod = "cq_performance_transaction_method"
	tagMethod             = "method"
	tagMethodRead         = "READ"
	tagMethodWrite        = "WRITE"
	fieldSucceedCounter   = "succeed_request_counter"
	fieldFailedCounter    = "failed_request_counter"

	// レイテンシ: flux-api-reference.md section 2.4 と Appendix B item 4。
	//
	// TODO(実機検証): プレースホルダー。id tag の値は未文書化 -
	// docs/api-research/flux-api-reference.md section 2.4/Appendix B.4
	// 参照。実機クラスタで id tag の実際の値を列挙し（例:
	// cq_performance_latency への無フィルタクエリ）、
	// obs_perf_{read,write}_latency_seconds に依存する前にこれらを
	// 置き換えること。
	measLatency = "cq_performance_latency"
	tagID       = "id"
	tagIDRead   = "read"
	tagIDWrite  = "write"
	// fieldP50 を代表的なレイテンシ値として採用している: docs/design.md の
	// 契約テーブルには obs_perf_{read,write}_latency_seconds のパーセンタイル
	// ラベルの規定がないため、p50 はやや恣意的に選んだもの。p99 や
	// パーセンタイルラベル付きのペアにすべきかもしれない。実データが
	// 得られたら見直すこと。
	fieldP50 = "p50"

	// エラー: flux-api-reference.md section 3.2。cq_performance_error_head
	// （単なる cq_performance_error ではなく）を選んだのは、これが
	// tag（head、つまりプロトコル、例: S3）を持ち、かつ単一クエリで
	// system_errors/user_errors 両方の field を持つ唯一のエラー measurement
	// バリアントだから。これにより category と error_type の両方を
	// 文字列パースなしで1つの measurement から直接得られる -
	// docs/design.md が求める、旧来の文字列分割ベースの
	// transactionErrors.types パース廃止という要件を満たす。
	//
	// docs/design.md の契約テーブルは obs_perf_transaction_errors のラベルを
	// "category, error_type" と命名しているだけで、その正確な出所を
	// 定義していない。そのためこのマッピング（category は _field から、
	// error_type は head tag から）は検討の上での未検証の解釈である。
	// 実機クラスタでのテストによりこのマッピングが誤りだと判明した場合の
	// 代替 measurement: cq_performance_error_code (tag code) と
	// cq_performance_error_ns (tag namespace)。
	measErrorHead     = "cq_performance_error_head"
	tagHead           = "head"
	fieldSystemErrors = "system_errors"
	fieldUserErrors   = "user_errors"
)

// TODO(実機検証): 以下では帯域幅/スループット/エラーの単位変換を一切
// 行っていない（Flux の生の float 値をそのまま渡す）- docs/design.md の
// 単位変換に関する注記は、レイテンシの ms->s と他所で使う GB/KB 変換にしか
// 触れておらず、total_read_requests_size / total_write_requests_size /
// system_errors / user_errors の単位は文書化されていない。そのため
// これらはすでに目的の単位（それぞれ bytes/sec, count）であると仮定している。
// 実機クラスタで確認すること。

// fluxDuration は d を Flux の duration リテラルとして整形する。
// time.Duration.String() を直接使わないのは、Flux の duration リテラルの
// 文法が認識しない "µs"/"ns" という単位サフィックスを出力しうるため。
// 秒単位に丸め、非ゼロの h/m/s の要素のみを含める（丸めた結果が0なら
// 常に少なくとも "0s" を含める）。
func fluxDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d == 0 {
		return "0s"
	}

	var sb strings.Builder
	if d < 0 {
		sb.WriteByte('-')
		d = -d
	}

	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		fmt.Fprintf(&sb, "%dh", h)
	}
	if m > 0 {
		fmt.Fprintf(&sb, "%dm", m)
	}
	if s > 0 {
		fmt.Fprintf(&sb, "%ds", s)
	}
	return sb.String()
}

// fluxLastQuery builds a Flux query selecting the most recent value(s) of
// the given measurement over the last rangeSpan, with no tag filter, e.g.:
//
//	from(bucket:"monitoring_vdc") |> range(start: -5m) |> filter(fn: (r) => r._measurement == "cq_performance_throughput") |> last()
func fluxLastQuery(rangeSpan, measurement string) string {
	return fmt.Sprintf(
		`from(bucket:%q) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %q) |> last()`,
		perfBucket, rangeSpan, measurement,
	)
}

// fluxLastQueryWithTag builds a Flux query like fluxLastQuery, additionally
// ANDing in an exact-match filter on the given tag, e.g.:
//
//	from(bucket:"monitoring_vdc") |> range(start: -5m) |> filter(fn: (r) => r._measurement == "cq_performance_transaction_method" and r.method == "READ") |> last()
func fluxLastQueryWithTag(rangeSpan, measurement, tag, tagValue string) string {
	return fmt.Sprintf(
		`from(bucket:%q) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %q and r.%s == %q) |> last()`,
		perfBucket, rangeSpan, measurement, tag, tagValue,
	)
}

// ---------------------------------------------------------------------
// メトリクスディスクリプタ、パース、登録。
// ---------------------------------------------------------------------

var (
	perfReadLatencyDesc = prometheus.NewDesc(
		"obs_perf_read_latency_seconds",
		"Read transaction latency (p50) for the VDC, in seconds.",
		[]string{"vdc"}, nil,
	)
	perfWriteLatencyDesc = prometheus.NewDesc(
		"obs_perf_write_latency_seconds",
		"Write transaction latency (p50) for the VDC, in seconds.",
		[]string{"vdc"}, nil,
	)
	perfReadBytesDesc = prometheus.NewDesc(
		"obs_perf_read_bytes_per_second",
		"Read throughput for the VDC, in bytes per second.",
		[]string{"vdc"}, nil,
	)
	perfWriteBytesDesc = prometheus.NewDesc(
		"obs_perf_write_bytes_per_second",
		"Write throughput for the VDC, in bytes per second.",
		[]string{"vdc"}, nil,
	)
	perfReadTPSDesc = prometheus.NewDesc(
		"obs_perf_read_transactions_per_second",
		"Read transactions per second for the VDC (succeeded + failed).",
		[]string{"vdc"}, nil,
	)
	perfWriteTPSDesc = prometheus.NewDesc(
		"obs_perf_write_transactions_per_second",
		"Write transactions per second for the VDC (succeeded + failed).",
		[]string{"vdc"}, nil,
	)
	perfErrorsDesc = prometheus.NewDesc(
		"obs_perf_transaction_errors",
		"Transaction error count for the VDC by category and error type.",
		[]string{"vdc", "category", "error_type"}, nil,
	)
)

func init() {
	Registry["perf"] = collectPerf
}

// collectPerf は Registry["perf"] を実装する。vdc ラベルは run.LocalZone
// （メモ化済み - 失敗時は "unknown" にフォールバックする。きれいな vdc
// ラベルがなくても Flux データ自体は有用なため、これによりスクレイプ全体を
// 中断することはない）で決定した後、上の定数ブロックで説明した6つの
// Flux クエリを並行実行する（metering.go の WaitGroup ファンアウトパターンを
// 踏襲。各クエリは互いに独立しており直列化する理由がない）。
// docs/design.md のコレクターエラー処理方針に従い、非nilのエラーを返すのは
// 結果のメトリクススライスが完全に空で、かつ少なくとも1つのクエリ自体が
// 失敗した場合（空/該当なしの結果セットではなく、ネットワーク/パースエラー）
// のみ。それ以外の場合は収集できたもの全てを登録し nil を返す。個々の
// クエリの失敗は Settings.Logger.Warn でログに残す（ベストエフォート:
// 1つの不良クエリが他のクエリのメトリクスを巻き込んで失わせてはならない）。
func collectPerf(ctx context.Context, run *Run, registry *prometheus.Registry) error {
	vdc := "unknown"
	if lz, err := run.LocalZone(ctx); err == nil {
		vdc = lz.Name
	}

	rangeSpan := fluxDuration(run.Settings.PerfRange)

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		metrics  []prometheus.Metric
		firstErr error
	)

	// runQuery は query を実行し、結果を返す。失敗は記録され
	// （下の空結果フォールバック用に最初のエラーを優先する）ログにも
	// 残されるが、他の実行中のクエリを中断することはない。
	runQuery := func(query string) []obsclient.FluxResult {
		results, err := run.Client.Query(ctx, query)
		if err != nil {
			mu.Lock()
			if firstErr == nil {
				firstErr = err
			}
			mu.Unlock()
			if run.Settings.Logger != nil {
				run.Settings.Logger.Warn("perf: flux query failed", "query", query, "err", err)
			}
			return nil
		}
		return results
	}

	addMetrics := func(m ...prometheus.Metric) {
		mu.Lock()
		metrics = append(metrics, m...)
		mu.Unlock()
	}

	goQuery := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	goQuery(func() {
		for _, r := range runQuery(fluxLastQuery(rangeSpan, measThroughput)) {
			switch r.Field {
			case fieldReadBytes:
				addMetrics(prometheus.MustNewConstMetric(perfReadBytesDesc, prometheus.GaugeValue, r.Value, vdc))
			case fieldWriteBytes:
				addMetrics(prometheus.MustNewConstMetric(perfWriteBytesDesc, prometheus.GaugeValue, r.Value, vdc))
			}
		}
	})

	goQuery(func() {
		if v, ok := sumTPS(runQuery(fluxLastQueryWithTag(rangeSpan, measTransactionMethod, tagMethod, tagMethodRead))); ok {
			addMetrics(prometheus.MustNewConstMetric(perfReadTPSDesc, prometheus.GaugeValue, v, vdc))
		}
	})
	goQuery(func() {
		if v, ok := sumTPS(runQuery(fluxLastQueryWithTag(rangeSpan, measTransactionMethod, tagMethod, tagMethodWrite))); ok {
			addMetrics(prometheus.MustNewConstMetric(perfWriteTPSDesc, prometheus.GaugeValue, v, vdc))
		}
	})

	goQuery(func() {
		if v, ok := findField(runQuery(fluxLastQueryWithTag(rangeSpan, measLatency, tagID, tagIDRead)), fieldP50); ok {
			addMetrics(prometheus.MustNewConstMetric(perfReadLatencyDesc, prometheus.GaugeValue, v/1000, vdc))
		}
	})
	goQuery(func() {
		if v, ok := findField(runQuery(fluxLastQueryWithTag(rangeSpan, measLatency, tagID, tagIDWrite)), fieldP50); ok {
			addMetrics(prometheus.MustNewConstMetric(perfWriteLatencyDesc, prometheus.GaugeValue, v/1000, vdc))
		}
	})

	goQuery(func() {
		for _, r := range runQuery(fluxLastQuery(rangeSpan, measErrorHead)) {
			var category string
			switch r.Field {
			case fieldSystemErrors:
				category = "system"
			case fieldUserErrors:
				category = "user"
			default:
				continue
			}
			addMetrics(prometheus.MustNewConstMetric(perfErrorsDesc, prometheus.GaugeValue, r.Value, vdc, category, r.Tags[tagHead]))
		}
	})

	wg.Wait()

	if len(metrics) == 0 && firstErr != nil {
		return firstErr
	}

	registry.MustRegister(newConstCollector(metrics))
	return nil
}

// sumTPS は、method でフィルタした cq_performance_transaction_method クエリの
// succeed_request_counter と failed_request_counter の両フィールドを
// 合算することで、失敗も含めた「1秒あたりのリクエスト数」を再構成する
// （成功のみを数えるより、旧 dashboard フィールドの意味に近い）。
func sumTPS(results []obsclient.FluxResult) (float64, bool) {
	var sum float64
	found := false
	for _, r := range results {
		switch r.Field {
		case fieldSucceedCounter, fieldFailedCounter:
			sum += r.Value
			found = true
		}
	}
	return sum, found
}

// findField は指定した field 名を持つ最初の結果の値を返す。一致するものが
// なければ (0, false) を返す。
func findField(results []obsclient.FluxResult, field string) (float64, bool) {
	for _, r := range results {
		if r.Field == field {
			return r.Value, true
		}
	}
	return 0, false
}
