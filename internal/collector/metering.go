// This file implements Registry["metering"]: per-namespace quota/usage
// metrics derived from GET /object/namespaces (obsclient.Client.
// ListNamespaces), GET /object/namespaces/namespace/{ns}/quota
// (GetNamespaceQuota) and GET /object/billing/namespace/{ns}/info
// (GetNamespaceBilling), per docs/design.md's metering collector contract
// table. Deliberately excluded from DefaultCollectors (probe.go) since it
// scales with namespace count.
package collector

import (
	"context"
	"log/slog"
	"sync"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
)

// kbToBytes converts obsclient.NamespaceBillingInfo.TotalSize to bytes.
// client.GetNamespaceBilling always requests sizeunit=KB (see
// internal/obsclient/metering.go), so TotalSize is in KB; docs/design.md
// explicitly calls for converting this to bytes ("billing の total_size は
// sizeunit=KB 指定（旧コード踏襲）→ bytes へ換算").
const kbToBytes = 1e3

var (
	meteringQuotaDesc = prometheus.NewDesc(
		"obs_metering_namespace_quota_bytes",
		"Configured namespace quota, in bytes, by quota type.",
		[]string{"namespace", "quota"}, nil,
	)
	meteringUsedDesc = prometheus.NewDesc(
		"obs_metering_namespace_used_bytes",
		"Namespace usage as reported by the billing API, in bytes.",
		[]string{"namespace"}, nil,
	)
	meteringObjectsDesc = prometheus.NewDesc(
		"obs_metering_namespace_objects",
		"Number of objects in the namespace as reported by the billing API.",
		[]string{"namespace"}, nil,
	)
)

func init() {
	Registry["metering"] = collectMetering
}

// collectMetering implements Registry["metering"]. ListNamespaces is the
// only call whose failure means nothing can be produced; per-namespace
// quota/billing calls are best-effort and run with bounded concurrency
// (run.Settings.MeteringConcurrency).
func collectMetering(ctx context.Context, run *Run, registry *prometheus.Registry) error {
	namespaces, err := run.Client.ListNamespaces(ctx)
	if err != nil {
		return err
	}

	concurrency := run.Settings.MeteringConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		metrics []prometheus.Metric
	)

	for _, ns := range namespaces {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			local := meteringNamespaceMetrics(ctx, run.Client, name, run.Settings.Logger)

			mu.Lock()
			metrics = append(metrics, local...)
			mu.Unlock()
		}(ns.Name)
	}
	wg.Wait()

	registry.MustRegister(newConstCollector(metrics))
	return nil
}

// meteringNamespaceMetrics collects the quota and billing metrics for a
// single namespace. Both API calls are best-effort: an error from either
// simply skips that namespace's contribution for that metric family
// (logged via logger.Warn if logger is non-nil), without aborting the rest
// of the scrape.
func meteringNamespaceMetrics(ctx context.Context, client *obsclient.Client, namespace string, logger *slog.Logger) []prometheus.Metric {
	var metrics []prometheus.Metric

	if q, err := client.GetNamespaceQuota(ctx, namespace); err == nil {
		// blockSize == 0 is ECS/ObjectScale's sentinel for "no quota
		// configured" (ECS minimum configurable quota is 1GB - see
		// dell-ecs/admin-guide/05-namespaces.md), not an actual zero-byte
		// quota, so quota metrics (both block and notification) are only
		// emitted when blockSize > 0.
		if v, err := q.BlockSize.Float64(); err == nil && v > 0 {
			metrics = append(metrics, prometheus.MustNewConstMetric(meteringQuotaDesc, prometheus.GaugeValue, v*gbToBytes, namespace, "block"))
			if v, err := q.NotificationSize.Float64(); err == nil {
				metrics = append(metrics, prometheus.MustNewConstMetric(meteringQuotaDesc, prometheus.GaugeValue, v*gbToBytes, namespace, "notification"))
			}
		}
	} else if logger != nil {
		logger.Warn("metering: get namespace quota failed", "namespace", namespace, "err", err)
	}

	if b, err := client.GetNamespaceBilling(ctx, namespace); err == nil {
		if v, err := b.TotalSize.Float64(); err == nil {
			metrics = append(metrics, prometheus.MustNewConstMetric(meteringUsedDesc, prometheus.GaugeValue, v*kbToBytes, namespace))
		}
		if v, err := b.TotalObjects.Float64(); err == nil {
			metrics = append(metrics, prometheus.MustNewConstMetric(meteringObjectsDesc, prometheus.GaugeValue, v, namespace))
		}
	} else if logger != nil {
		logger.Warn("metering: get namespace billing failed", "namespace", namespace, "err", err)
	}

	return metrics
}
