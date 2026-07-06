// このファイルは Registry["replication"] を実装する:
// GET /dashboard/zones/localzone/replicationgroups
// （obsclient.Client.GetReplicationGroups 経由）から得られるメトリクス。
// docs/design.md の replication コレクター契約テーブルに基づく。
package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	replicationIngressDesc = prometheus.NewDesc(
		"obs_replication_ingress_bytes_per_second",
		"Replication ingress traffic for the replication group, in bytes per second.",
		[]string{"rg"}, nil,
	)
	replicationEgressDesc = prometheus.NewDesc(
		"obs_replication_egress_bytes_per_second",
		"Replication egress traffic for the replication group, in bytes per second.",
		[]string{"rg"}, nil,
	)
	replicationPendingRepoDesc = prometheus.NewDesc(
		"obs_replication_pending_repo_bytes",
		"Repo chunks pending replication for the replication group, in bytes.",
		[]string{"rg"}, nil,
	)
	replicationPendingJournalDesc = prometheus.NewDesc(
		"obs_replication_pending_journal_bytes",
		"Journal chunks pending replication for the replication group, in bytes.",
		[]string{"rg"}, nil,
	)
	replicationPendingXorDesc = prometheus.NewDesc(
		"obs_replication_pending_xor_bytes",
		"XOR chunks pending replication for the replication group, in bytes.",
		[]string{"rg"}, nil,
	)
	replicationRPODesc = prometheus.NewDesc(
		"obs_replication_rpo_timestamp_seconds",
		"Recovery point objective timestamp for the replication group, as epoch seconds.",
		[]string{"rg"}, nil,
	)
)

func init() {
	Registry["replication"] = collectReplication
}

// collectReplication は Registry["replication"] を実装する。呼び出すのは
// GetReplicationGroups のみで、失敗すると何も生成できない。1つのグループ
// 内では、各フィールドをベストエフォートで個別に出力する。
func collectReplication(ctx context.Context, run *Run, registry *prometheus.Registry) error {
	groups, err := run.Client.GetReplicationGroups(ctx)
	if err != nil {
		return err
	}

	var metrics []prometheus.Metric
	for _, g := range groups {
		rg := g.Name

		if v, err := g.ReplicationIngressTraffic.Float64(); err == nil {
			metrics = append(metrics, prometheus.MustNewConstMetric(replicationIngressDesc, prometheus.GaugeValue, v, rg))
		}
		if v, err := g.ReplicationEgressTraffic.Float64(); err == nil {
			metrics = append(metrics, prometheus.MustNewConstMetric(replicationEgressDesc, prometheus.GaugeValue, v, rg))
		}
		if v, err := g.ChunksRepoPendingReplicationTotalSize.Float64(); err == nil {
			metrics = append(metrics, prometheus.MustNewConstMetric(replicationPendingRepoDesc, prometheus.GaugeValue, v, rg))
		}
		if v, err := g.ChunksJournalPendingReplicationTotalSize.Float64(); err == nil {
			metrics = append(metrics, prometheus.MustNewConstMetric(replicationPendingJournalDesc, prometheus.GaugeValue, v, rg))
		}
		if v, err := g.ChunksPendingXorTotalSize.Float64(); err == nil {
			metrics = append(metrics, prometheus.MustNewConstMetric(replicationPendingXorDesc, prometheus.GaugeValue, v, rg))
		}
		// ReplicationRpoTimestamp は（types.go に記載の通り）observed 上
		// epoch ミリ秒であるため、docs/design.md に従い1000で割って
		// epoch 秒に変換する。
		if v, err := g.ReplicationRpoTimestamp.Float64(); err == nil {
			metrics = append(metrics, prometheus.MustNewConstMetric(replicationRPODesc, prometheus.GaugeValue, v/1000, rg))
		}
	}

	registry.MustRegister(newConstCollector(metrics))
	return nil
}
