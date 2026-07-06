// このファイルは Registry["cluster"] を実装する: GET /dashboard/zones/localzone
// （obsclient.Client.GetLocalZone 経由）と、ベストエフォートで
// GET /vdc/nodes（obsclient.Client.GetNodes 経由）から得られるメトリクス。
// docs/design.md の cluster コレクター契約テーブルに基づく。
package collector

import (
	"context"
	"strings"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
)

// gbToBytes は dashboard API のディスク容量 "Space" 値をバイトに変換する。
// 単位はバイナリ GiB ではなく十進 GB（10^9 バイト）と仮定している。
//
// TODO(実機検証): 本プロジェクトで確認したマニュアル
// (~/miyamado/user-manuals/dell-ecs/monitoring-guide/04-advanced-monitoring.md,
// ~/miyamado/user-manuals/dell-objectscale/admin-guide/15-advanced-monitoring.md、
// および dashboard API に触れている admin-guide/03-monitoring.md /
// admin-guide/14-advanced-monitoring.md）には、
// diskSpaceTotalCurrent / diskSpaceFreeCurrent の Space フィールドの単位が
// フィールドレベルで記載されていない。/dashboard/zones/localzone
// エンドポイントのパスへの言及があるのみで、フィールドレベルのスキーマや
// 単位は不明。本タスクの既定フォールバックに従い GB=10^9（十進）と
// 仮定しているが、obs_cluster_capacity_*_bytes に依存する前に、実機で
// これらの値が十進 GB かバイナリ GiB かを確認すること。
const gbToBytes = 1e9 // GB = 10^9 bytes.

var (
	clusterNodesDesc = prometheus.NewDesc(
		"obs_cluster_nodes",
		"Number of nodes in the VDC by health state.",
		[]string{"vdc", "state"}, nil,
	)
	clusterDisksDesc = prometheus.NewDesc(
		"obs_cluster_disks",
		"Number of disks in the VDC by health state.",
		[]string{"vdc", "state"}, nil,
	)
	clusterCapacityTotalDesc = prometheus.NewDesc(
		"obs_cluster_capacity_total_bytes",
		"Total raw disk capacity of the VDC, in bytes.",
		[]string{"vdc"}, nil,
	)
	clusterCapacityFreeDesc = prometheus.NewDesc(
		"obs_cluster_capacity_free_bytes",
		"Free raw disk capacity of the VDC, in bytes.",
		[]string{"vdc"}, nil,
	)
	clusterAlertsDesc = prometheus.NewDesc(
		"obs_cluster_alerts_unacknowledged",
		"Number of unacknowledged alerts in the VDC by severity.",
		[]string{"vdc", "severity"}, nil,
	)
	clusterInfoDesc = prometheus.NewDesc(
		"obs_cluster_info",
		"Constant 1, labeled with the cluster software version and inferred product.",
		[]string{"vdc", "version", "product"}, nil,
	)
)

func init() {
	Registry["cluster"] = collectCluster
}

// collectCluster は Registry["cluster"] を実装する。失敗すると何も
// 生成できなくなるのは GetLocalZone だけで、GetNodes はベストエフォート
// （obs_cluster_info の version/product ラベルにのみ使われる）。両方とも
// run のメモ化アクセサ経由なので、perf/node コレクターも実行される
// /probe 呼び出しでこれらの API 呼び出しが重複することはない
// （docs/design.md）。
func collectCluster(ctx context.Context, run *Run, registry *prometheus.Registry) error {
	lz, err := run.LocalZone(ctx)
	if err != nil {
		return err
	}
	vdc := lz.Name

	var metrics []prometheus.Metric

	if v, err := lz.NumGoodNodes.Float64(); err == nil {
		metrics = append(metrics, prometheus.MustNewConstMetric(clusterNodesDesc, prometheus.GaugeValue, v, vdc, "good"))
	}
	if v, err := lz.NumBadNodes.Float64(); err == nil {
		metrics = append(metrics, prometheus.MustNewConstMetric(clusterNodesDesc, prometheus.GaugeValue, v, vdc, "bad"))
	}

	if v, err := lz.NumGoodDisks.Float64(); err == nil {
		metrics = append(metrics, prometheus.MustNewConstMetric(clusterDisksDesc, prometheus.GaugeValue, v, vdc, "good"))
	}
	if v, err := lz.NumBadDisks.Float64(); err == nil {
		metrics = append(metrics, prometheus.MustNewConstMetric(clusterDisksDesc, prometheus.GaugeValue, v, vdc, "bad"))
	}

	if v, ok := obsclient.FirstSpace(lz.DiskSpaceTotalCurrent); ok {
		metrics = append(metrics, prometheus.MustNewConstMetric(clusterCapacityTotalDesc, prometheus.GaugeValue, v*gbToBytes, vdc))
	}
	if v, ok := obsclient.FirstSpace(lz.DiskSpaceFreeCurrent); ok {
		metrics = append(metrics, prometheus.MustNewConstMetric(clusterCapacityFreeDesc, prometheus.GaugeValue, v*gbToBytes, vdc))
	}

	if v, ok := obsclient.FirstCount(lz.AlertsNumUnackCritical); ok {
		metrics = append(metrics, prometheus.MustNewConstMetric(clusterAlertsDesc, prometheus.GaugeValue, v, vdc, "critical"))
	}
	if v, ok := obsclient.FirstCount(lz.AlertsNumUnackError); ok {
		metrics = append(metrics, prometheus.MustNewConstMetric(clusterAlertsDesc, prometheus.GaugeValue, v, vdc, "error"))
	}
	if v, ok := obsclient.FirstCount(lz.AlertsNumUnackInfo); ok {
		metrics = append(metrics, prometheus.MustNewConstMetric(clusterAlertsDesc, prometheus.GaugeValue, v, vdc, "info"))
	}
	if v, ok := obsclient.FirstCount(lz.AlertsNumUnackWarning); ok {
		metrics = append(metrics, prometheus.MustNewConstMetric(clusterAlertsDesc, prometheus.GaugeValue, v, vdc, "warning"))
	}

	version := "unknown"
	if nodes, err := run.Nodes(ctx); err == nil && len(nodes) > 0 {
		version = nodes[0].Version
	}
	product := inferProduct(version)
	metrics = append(metrics, prometheus.MustNewConstMetric(clusterInfoDesc, prometheus.GaugeValue, 1, vdc, version, product))

	registry.MustRegister(newConstCollector(metrics))
	return nil
}

// inferProduct はクラスタソフトウェアのバージョン文字列からプロダクト
// ファミリーを推測する。
//
// この exporter が呼ぶ API レスポンスにはどれも明示的な product フィールドが
// ないため、docs/design.md 自身のバージョン採番（ECS 3.8.1.x /
// ObjectScale 4.1.0.x）からこのヒューリスティックを導出している。
//
// TODO(実機検証): obs_cluster_info の product ラベルに依存する前に、
// 各プロダクトファミリーの実機クラスタでこのバージョンプレフィックスの
// ヒューリスティックを確認すること。
func inferProduct(version string) string {
	switch {
	case strings.HasPrefix(version, "3."):
		return "ecs"
	case strings.HasPrefix(version, "4."):
		return "objectscale"
	default:
		return "unknown"
	}
}
