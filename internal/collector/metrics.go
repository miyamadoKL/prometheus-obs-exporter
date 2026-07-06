// Package collector (this file) provides a small shared helper used by
// every concrete collector (cluster.go, replication.go, node.go,
// metering.go, perf.go) and by probe.go itself (for
// obs_scrape_success / obs_scrape_duration_seconds): an adapter that turns
// a plain slice of already-built prometheus.Metric values into a
// prometheus.Collector, so it can be registered into a
// *prometheus.Registry via MustRegister.
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
