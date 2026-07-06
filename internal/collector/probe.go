// Package collector は GET /probe から呼び出される Prometheus コレクター群を
// 実装する（docs/design.md の Phase C/D 参照）。このファイルは
// cmd/obs-exporter/main.go の /probe ハンドラが利用するディスパッチ
// インターフェースと、cluster/replication/node/metering/perf の各具体的な
// コレクター（cluster.go, replication.go, node.go, metering.go, perf.go）を
// 定義する。
package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
)

// DefaultCollectors は /probe リクエストで collectors クエリパラメータが
// 省略された場合に使われる。metering は意図的に除外している
// （docs/design.md 参照: namespace 数に比例してスケールするため明示的な
// 指定を必須とする）。
var DefaultCollectors = []string{"cluster", "replication", "node", "perf"}

// Settings はリクエストスコープのコレクター設定を保持し、Run を介して
// /probe の各呼び出しに渡される。かつて node.go / metering.go / perf.go に
// あったパッケージレベルの DTStatsEnabled / MeteringConcurrency /
// PerfRange 変数を置き換えるもの。それらはプロセスグローバルで
// リクエストごとに変えられず、並列テストでも安全に扱えなかった。
type Settings struct {
	// DTStatsEnabled は node コレクターの DT-stats/ping スクレイプを
	// 切り替える（docs/design.md: --collector.node.dt-stats /
	// OBS_EXPORTER_COLLECTOR_NODE_DT_STATS）。
	DTStatsEnabled bool
	// MeteringConcurrency は metering コレクターが同時に問い合わせる
	// namespace 数の上限（docs/design.md: --collector.metering.concurrency）。
	MeteringConcurrency int
	// PerfRange は perf コレクターの各 Flux クエリで使う遡及期間
	// （docs/design.md: --collector.perf.range）。
	PerfRange time.Duration
	// Logger はコレクター単位・項目単位（ベストエフォート）の失敗ログを
	// 受け取る（docs/design.md のエラー可視化要件）。nil の場合はログを
	// 出力しない。
	Logger *slog.Logger
}

// Run は単一の /probe 呼び出しに必要な全てをまとめる: 認証済みクライアント、
// このリクエストの Settings、そして複数のコレクターが必要とする API 呼び出し
// （LocalZone, Nodes）のメモ化アクセサ。これにより、複数のコレクターが同じ
// データを求めても、1回の /probe 呼び出しにつきそれぞれのリクエストは
// 最大1回しか発行されない（docs/design.md; NewRun も参照）。
type Run struct {
	Client   *obsclient.Client
	Settings Settings

	localZone func() (*obsclient.LocalZone, error)
	nodes     func() ([]obsclient.Node, error)
}

// NewRun は単一の /probe 呼び出し用の Run を構築する。ctx は以下の
// メモ化された LocalZone/Nodes アクセサの生存期間にわたってキャプチャ
// される。この Run に対して呼ばれるすべてのコレクターは同じ ctx で
// 呼び出される（Probe を参照）ので、ここで一度だけ束縛することは、
// sync.OnceValues の引数なし関数という形に適合させつつ、各呼び出しに
// ctx を渡すのと等価になる。
func NewRun(ctx context.Context, client *obsclient.Client, settings Settings) *Run {
	return &Run{
		Client:   client,
		Settings: settings,
		localZone: sync.OnceValues(func() (*obsclient.LocalZone, error) {
			return client.GetLocalZone(ctx)
		}),
		nodes: sync.OnceValues(func() ([]obsclient.Node, error) {
			return client.GetNodes(ctx)
		}),
	}
}

// LocalZone は GET /dashboard/zones/localzone の結果を返す。この Run に
// つき何回呼ばれても取得は最大1回。ctx はシグネチャの対称性・将来の
// 柔軟性のために受け取っているだけで、内部のリクエストは常に Run 構築時の
// ctx を使う（NewRun 参照）。
func (r *Run) LocalZone(ctx context.Context) (*obsclient.LocalZone, error) {
	return r.localZone()
}

// Nodes は GET /vdc/nodes の結果を返す。この Run につき何回呼ばれても
// 取得は最大1回。ctx の扱いについては LocalZone を参照。
func (r *Run) Nodes(ctx context.Context) ([]obsclient.Node, error) {
	return r.nodes()
}

// Func は、具体的なコレクター実装が満たすべき形。run（認証済み
// クライアント、このリクエストの Settings、メモ化された LocalZone/Nodes
// アクセサ）を使って単一コレクターのスクレイプを実行し、そのメトリクスを
// registry に直接登録する。エラーはメトリクスを一切生成できなかった
// 失敗のためだけに使うべきで、部分的なデータはエラーにせずベストエフォート
// で出力すること。
type Func func(ctx context.Context, run *Run, registry *prometheus.Registry) error

// Registry は、/probe の "collectors" クエリパラメータで受け付ける
// コレクター名をその実装にマッピングする。cluster.go / replication.go /
// node.go / metering.go / perf.go がそれぞれ自身の init() からここに
// 登録する。
var Registry = map[string]Func{}

var (
	scrapeSuccessDesc = prometheus.NewDesc(
		"obs_scrape_success",
		"1 if the named collector completed successfully against this target, 0 otherwise.",
		[]string{"collector"}, nil,
	)
	scrapeDurationDesc = prometheus.NewDesc(
		"obs_scrape_duration_seconds",
		"Time in seconds the named collector took to scrape this target.",
		[]string{"collector"}, nil,
	)
)

// Probe は要求された各コレクター（Registry で名前引きする）を client に
// 対して実行し、そのメトリクスを registry に登録するとともに、
// コレクターごとの obs_scrape_success / obs_scrape_duration_seconds の
// ペアを記録する（docs/design.md: "collector単位で失敗を隔離し...に反映する"）。
//
// 呼び出し全体につき単一の Run を構築する（NewRun 参照）ので、複数の
// 要求されたコレクターが必要とする場合でも LocalZone/Nodes の取得は
// 最大1回で済む。
//
// Registry に存在しない未知のコレクター名は、リクエスト全体を中断するのではなく
// obs_scrape_success=0 として記録・ログ出力する。これにより、1つの
// 不正・未実装のコレクター名が、それ以外は成功しているスクレイプ全体を
// 失敗させることはない。
//
// Probe 自身は、コレクターの欠落・失敗に対して非nilのエラーを返すことはない。
// エラーはプログラマのミス（例: nil client）のために予約されている。
func Probe(ctx context.Context, client *obsclient.Client, settings Settings, collectors []string, registry *prometheus.Registry) error {
	run := NewRun(ctx, client, settings)

	successMetrics := make([]prometheus.Metric, 0, len(collectors))
	durationMetrics := make([]prometheus.Metric, 0, len(collectors))

	for _, name := range collectors {
		start := time.Now()

		success := 0.0
		if fn, ok := Registry[name]; ok {
			if err := fn(ctx, run, registry); err == nil {
				success = 1.0
			} else if settings.Logger != nil {
				settings.Logger.Error("collector failed", "collector", name, "err", err)
			}
		} else if settings.Logger != nil {
			settings.Logger.Error("unknown collector", "collector", name)
		}

		duration := time.Since(start).Seconds()
		successMetrics = append(successMetrics, prometheus.MustNewConstMetric(scrapeSuccessDesc, prometheus.GaugeValue, success, name))
		durationMetrics = append(durationMetrics, prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, duration, name))
	}

	registry.MustRegister(newConstCollector(append(successMetrics, durationMetrics...)))
	return nil
}
