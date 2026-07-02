// This file implements Registry["replication"]: metrics derived from
// GET /dashboard/zones/localzone/replicationgroups (via
// obsclient.Client.GetReplicationGroups), per docs/design.md's replication
// collector contract table.
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

// collectReplication implements Registry["replication"]. GetReplicationGroups
// is the only call; its failure means nothing can be produced. Within a
// group, each field is emitted independently on a best-effort basis.
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
		// ReplicationRpoTimestamp is documented (types.go) as observed to be
		// epoch milliseconds; divide by 1000 to get epoch seconds per
		// docs/design.md.
		if v, err := g.ReplicationRpoTimestamp.Float64(); err == nil {
			metrics = append(metrics, prometheus.MustNewConstMetric(replicationRPODesc, prometheus.GaugeValue, v/1000, rg))
		}
	}

	registry.MustRegister(newConstCollector(metrics))
	return nil
}
