// This file implements Registry["node"]: directory-table (DT) statistics /
// active-connection metrics derived from each node's management IP
// (obsclient.Client's GetNodes for the node list, via run's memoized
// accessor, and GetAllNodeDTStats for the per-node data via
// http://<node>:9101/stats/dt/DTInitStat and https://<node>:<objPort>/?ping),
// per docs/design.md's node collector contract table.
package collector

import (
	"context"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// obs_node_directory_tables{,_unready,_unknown}: named after what they
	// actually count (directory tables), not "obs_node_dt_total" - besides
	// being clearer, a Gauge named "_total" violates Prometheus naming
	// conventions (_total is reserved for counters).
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

// collectNode implements Registry["node"]. If run.Settings.DTStatsEnabled
// is false, it does nothing and returns nil (a deliberately empty,
// successful scrape). Otherwise it lists nodes (via run.Nodes, memoized -
// failure means nothing can be produced), queries DT stats/ping for every
// node's management IP (matching the pre-rewrite exporter's precedent -
// see git show HEAD:pkg/ecsclient/ecsclient.go's nodeListMgmtIP /
// retrieveNodeState), and emits metrics for every node that responded
// without error. A node whose DT-stats/ping call failed is logged via
// Settings.Logger.Warn and otherwise skipped (best-effort: one unreachable
// node must not drop the others' metrics).
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

// nodeDTMetrics converts a slice of obsclient.NodeDTStats into the four
// per-node obs_node_* Gauge metrics. It is factored out into its own
// function (rather than inlined into collectNode) specifically so it can be
// unit-tested directly: obsclient hardcodes port :9101 for DT stats, so
// there is no way to redirect that call to an httptest server.
//
// Stats with a non-nil Err are skipped entirely - a single unreachable node
// should not drop the others' metrics.
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
