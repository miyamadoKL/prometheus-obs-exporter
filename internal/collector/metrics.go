// Package collector（このファイル）は、全ての具体的コレクター
// （cluster.go, replication.go, node.go, metering.go, perf.go）と
// probe.go 自身（obs_scrape_success / obs_scrape_duration_seconds 用）が
// 使う小さな共有ヘルパーを提供する: 構築済みの prometheus.Metric の
// スライスを prometheus.Collector に変換するアダプタで、
// *prometheus.Registry へ MustRegister で登録できるようにする。
package collector

import "github.com/prometheus/client_golang/prometheus"

// constCollector adapts a fixed slice of already-built prometheus.Metric
// values (as produced by prometheus.MustNewConstMetric) into a
// prometheus.Collector.
type constCollector struct {
	metrics []prometheus.Metric
}

// newConstCollector wraps metrics (built via prometheus.MustNewConstMetric)
// for registration into a *prometheus.Registry.
func newConstCollector(metrics []prometheus.Metric) *constCollector {
	return &constCollector{metrics: metrics}
}

func (c *constCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range c.metrics {
		ch <- m.Desc()
	}
}

func (c *constCollector) Collect(ch chan<- prometheus.Metric) {
	for _, m := range c.metrics {
		ch <- m
	}
}
