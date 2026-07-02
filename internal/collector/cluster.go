// This file implements Registry["cluster"]: metrics derived from
// GET /dashboard/zones/localzone (via obsclient.Client.GetLocalZone) and,
// best-effort, GET /vdc/nodes (via obsclient.Client.GetNodes), per
// docs/design.md's cluster collector contract table.
package collector

import (
	"context"
	"strings"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
)

// gbToBytes converts the dashboard API's disk-space "Space" values to
// bytes, assuming decimal GB (10^9 bytes) rather than binary GiB.
//
// TODO(real-device verification): the manuals checked for this project
// (~/miyamado/user-manuals/dell-ecs/monitoring-guide/04-advanced-monitoring.md,
// ~/miyamado/user-manuals/dell-objectscale/admin-guide/15-advanced-monitoring.md,
// and the dashboard-API-focused admin-guide/03-monitoring.md /
// admin-guide/14-advanced-monitoring.md) do not document the unit of
// diskSpaceTotalCurrent / diskSpaceFreeCurrent's Space field at the field
// level - only bare mentions of the /dashboard/zones/localzone endpoint
// path exist, with no field-level schema or units. GB=10^9 (decimal) is
// assumed per the task's documented fallback; confirm against a live
// cluster whether these values are decimal GB or binary GiB before relying
// on obs_cluster_capacity_*_bytes.
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

// collectCluster implements Registry["cluster"]. GetLocalZone is the only
// call whose failure means nothing can be produced; GetNodes is
// best-effort (it only feeds obs_cluster_info's version/product labels). Both
// go through run's memoized accessors so a /probe call that also runs the
// perf/node collectors does not repeat these API calls (docs/design.md).
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

// inferProduct guesses the product family from the cluster software
// version string.
//
// None of the API responses this exporter calls expose an explicit product
// field; this heuristic is derived from docs/design.md's own version
// numbering (ECS 3.8.1.x / ObjectScale 4.1.0.x).
//
// TODO(real-device verification): confirm this version-prefix heuristic
// against a real cluster of each product family before relying on
// obs_cluster_info's product label.
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
