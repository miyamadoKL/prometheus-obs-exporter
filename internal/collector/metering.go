// このファイルは Registry["metering"] を実装する: GET /object/namespaces
// (obsclient.Client.ListNamespaces)、GET
// /object/namespaces/namespace/{ns}/quota (GetNamespaceQuota)、
// GET /object/billing/namespace/{ns}/info (GetNamespaceBilling) から得られる
// namespace ごとのクォータ/使用量メトリクス。docs/design.md の metering
// コレクター契約テーブルに基づく。namespace 数に比例してスケールするため、
// DefaultCollectors（probe.go）からは意図的に除外している。
package collector

import (
	"context"
	"log/slog"
	"sync"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
)

// kbToBytes は obsclient.NamespaceBillingInfo.TotalSize をバイトへ変換する。
// client.GetNamespaceBilling は常に sizeunit=KB を要求する
// （internal/obsclient/metering.go 参照）ので TotalSize は KB 単位。
// docs/design.md はこれをバイトへ変換することを明示的に求めている
// ("billing の total_size は sizeunit=KB 指定（旧コード踏襲）→ bytes へ換算")。
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

// collectMetering は Registry["metering"] を実装する。失敗すると何も
// 生成できなくなるのは ListNamespaces だけで、namespace ごとの
// quota/billing 呼び出しはベストエフォートかつ並行数を制限して実行する
// （run.Settings.MeteringConcurrency）。
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

// meteringNamespaceMetrics は単一 namespace のクォータ・課金メトリクスを
// 収集する。両方の API 呼び出しともベストエフォートで、どちらかがエラーに
// なった場合は単にその namespace のそのメトリクスファミリーへの寄与を
// スキップするだけ（logger が非nilなら logger.Warn でログ出力）で、
// スクレイプの残りを中断することはない。
func meteringNamespaceMetrics(ctx context.Context, client *obsclient.Client, namespace string, logger *slog.Logger) []prometheus.Metric {
	var metrics []prometheus.Metric

	if q, err := client.GetNamespaceQuota(ctx, namespace); err == nil {
		// blockSize == 0 は ECS/ObjectScale における「クォータ未設定」を
		// 表す番兵値であり（ECS の設定可能な最小クォータは 1GB -
		// dell-ecs/admin-guide/05-namespaces.md 参照）、実際に0バイトの
		// クォータという意味ではない。そのためクォータメトリクス
		// （block, notification 両方）は blockSize > 0 の場合のみ出力する。
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
