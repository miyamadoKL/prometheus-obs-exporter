// このファイルは Registry["node"] を実装する: 各ノードの管理IPから得られる
// ディレクトリテーブル（DT）統計・アクティブ接続数のメトリクス
// （ノード一覧は run のメモ化アクセサ経由の obsclient.Client.GetNodes、
// ノードごとのデータは http://<node>:9101/stats/dt/DTInitStat と
// https://<node>:<objPort>/?ping を叩く GetAllNodeDTStats）。
// docs/design.md の node コレクター契約テーブルに基づく。
package collector

import (
	"context"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// obs_node_directory_tables{,_unready,_unknown}: "obs_node_dt_total" では
	// なく実際にカウントしている対象（directory tables）にちなんで命名。
	// わかりやすさに加え、Gauge に "_total" と付けるのは Prometheus の
	// 命名規則違反でもある（_total は counter 用に予約されている）。
	nodeDirectoryTablesDesc = prometheus.NewDesc(
		"obs_node_directory_tables",
		"Total number of directory tables (DTs) reported by the node.",
		[]string{"node"}, nil,
	)
	nodeDirectoryTablesUnreadyDesc = prometheus.NewDesc(
		"obs_node_directory_tables_unready",
		"Number of unready directory tables (DTs) reported by the node.",
		[]string{"node"}, nil,
	)
	nodeDirectoryTablesUnknownDesc = prometheus.NewDesc(
		"obs_node_directory_tables_unknown",
		"Number of unknown directory tables (DTs) reported by the node.",
		[]string{"node"}, nil,
	)
	nodeActiveConnectionsDesc = prometheus.NewDesc(
		"obs_node_active_connections",
		"Active connection count reported by the node's ping endpoint.",
		[]string{"node"}, nil,
	)
)

func init() {
	Registry["node"] = collectNode
}

// collectNode は Registry["node"] を実装する。run.Settings.DTStatsEnabled が
// false なら何もせず nil を返す（意図的に空の、成功扱いのスクレイプ）。
// そうでなければノード一覧を取得し（run.Nodes、メモ化済み。失敗すると
// 何も生成できない）、各ノードの管理IPに対して DT stats/ping を問い合わせ
// （書き換え前の exporter の先例に合わせている - git show
// HEAD:pkg/ecsclient/ecsclient.go の nodeListMgmtIP / retrieveNodeState
// 参照）、エラーなく応答した各ノードのメトリクスを出力する。DT-stats/ping
// 呼び出しが失敗したノードは Settings.Logger.Warn でログを出しスキップする
// だけ（ベストエフォート: 到達不能な1ノードが他のノードのメトリクスを
// 巻き込んで失わせてはならない）。
func collectNode(ctx context.Context, run *Run, registry *prometheus.Registry) error {
	if !run.Settings.DTStatsEnabled {
		return nil
	}

	nodes, err := run.Nodes(ctx)
	if err != nil {
		return err
	}

	addrs := make([]string, 0, len(nodes))
	for _, n := range nodes {
		addrs = append(addrs, n.MgmtIP)
	}

	stats := run.Client.GetAllNodeDTStats(ctx, addrs)

	if run.Settings.Logger != nil {
		for _, stat := range stats {
			if stat.Err != nil {
				run.Settings.Logger.Warn("node DT-stats/ping query failed", "node", stat.Node, "err", stat.Err)
			}
		}
	}

	registry.MustRegister(newConstCollector(nodeDTMetrics(stats)))
	return nil
}

// nodeDTMetrics は obsclient.NodeDTStats のスライスを、ノードごとの4つの
// obs_node_* Gauge メトリクスに変換する。collectNode にインライン化せず
// 独立した関数として切り出しているのは、直接ユニットテストできるように
// するため: obsclient は DT stats のポートを :9101 にハードコードしており、
// その呼び出しを httptest サーバーへリダイレクトする手段がない。
//
// Err が非nilの stat は丸ごとスキップする - 到達不能な1ノードが他の
// ノードのメトリクスを失わせてはならない。
func nodeDTMetrics(stats []obsclient.NodeDTStats) []prometheus.Metric {
	var metrics []prometheus.Metric
	for _, stat := range stats {
		if stat.Err != nil {
			continue
		}
		metrics = append(metrics,
			prometheus.MustNewConstMetric(nodeDirectoryTablesDesc, prometheus.GaugeValue, stat.TotalDT, stat.Node),
			prometheus.MustNewConstMetric(nodeDirectoryTablesUnreadyDesc, prometheus.GaugeValue, stat.UnreadyDT, stat.Node),
			prometheus.MustNewConstMetric(nodeDirectoryTablesUnknownDesc, prometheus.GaugeValue, stat.UnknownDT, stat.Node),
			prometheus.MustNewConstMetric(nodeActiveConnectionsDesc, prometheus.GaugeValue, stat.ActiveConnections, stat.Node),
		)
	}
	return metrics
}
