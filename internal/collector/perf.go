// This file implements Registry["perf"]: VDC-aggregate performance metrics
// derived from the Flux time-series API (obsclient.Client.Query against
// POST /flux/api/external/v2/query), per docs/design.md's perf collector
// contract table.
//
// All Flux query definitions (measurement names, field names, tag
// names/values, and the query-building logic) live in the constants/table
// block immediately below, deliberately separated from the
// parsing/metric-emission logic further down, so that once real-cluster
// verification confirms or corrects any of these choices, only this block
// needs to change.
package collector

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------
// Flux query definitions - measurement / field / tag names and query
// construction. See docs/api-research/flux-api-reference.md for the source
// material; section numbers are cited next to each choice below.
// ---------------------------------------------------------------------

// perfBucket is the Flux bucket queried for every metric in this file.
// Confirmed identical on both ECS and ObjectScale
// (flux-api-reference.md section 6.1).
const perfBucket = "monitoring_vdc"

// The lookback window used for every Flux query below (always combined
// with |> last() to fetch only the most recent value in that window, per
// docs/design.md: "range(...) + last() で最新値のみ取得する") comes from
// run.Settings.PerfRange (docs/design.md: --collector.perf.range /
// OBS_EXPORTER_PERF_RANGE, default 5m - see internal/config/config.go's
// CollectorPerfRange).
//
// Note: the Flux API's range is documented as capped at 1h, but only in the
// ECS manual - flux-api-reference.md section 5.1 notes this constraint is
// unconfirmed for ObjectScale. internal/config/config.go rejects a
// configured range over 1h at startup.

const (
	// Throughput: flux-api-reference.md section 3.2 (cq_performance_*
	// catalog) and section 2.4's confidence table (confidence: high - the
	// field names are unambiguously read/write-separated).
	measThroughput  = "cq_performance_throughput"
	fieldReadBytes  = "total_read_requests_size"
	fieldWriteBytes = "total_write_requests_size"

	// TPS: flux-api-reference.md section 2.4 (confidence: medium - the
	// READ/WRITE tag values -> read/write-TPS mapping is inferred from the
	// method tag's documented values, not stated explicitly by either
	// manual).
	measTransactionMethod = "cq_performance_transaction_method"
	tagMethod             = "method"
	tagMethodRead         = "READ"
	tagMethodWrite        = "WRITE"
	fieldSucceedCounter   = "succeed_request_counter"
	fieldFailedCounter    = "failed_request_counter"

	// Latency: flux-api-reference.md sections 2.4 and Appendix B item 4.
	//
	// TODO(real-device verification): PLACEHOLDER, id tag values are
	// undocumented - see docs/api-research/flux-api-reference.md section
	// 2.4/Appendix B.4. Enumerate the id tag's actual values against a live
	// cluster (e.g. an unfiltered query on cq_performance_latency) and
	// replace these before relying on obs_perf_{read,write}_latency_seconds.
	measLatency = "cq_performance_latency"
	tagID       = "id"
	tagIDRead   = "read"
	tagIDWrite  = "write"
	// fieldP50 is used as "the" representative latency value: the
	// docs/design.md contract table has no percentile label for
	// obs_perf_{read,write}_latency_seconds, so p50 was chosen somewhat
	// arbitrarily. This could instead be p99 or a percentile-labeled pair;
	// revisit once real data is available.
	fieldP50 = "p50"

	// Errors: flux-api-reference.md section 3.2. cq_performance_error_head
	// (NOT the bare cq_performance_error) is chosen specifically because
	// it's the only error measurement variant that carries a tag (head,
	// i.e. protocol e.g. S3) AND has both system_errors/user_errors fields
	// in a single query, letting category and error_type both come
	// directly from one measurement without any string-parsing - satisfying
	// docs/design.md's requirement to abolish the old string-split-based
	// transactionErrors.types parsing.
	//
	// docs/design.md's contract table names the obs_perf_transaction_errors
	// labels "category, error_type" without defining their exact source, so
	// this mapping (category from _field, error_type from the head tag) is
	// a considered-but-unverified interpretation. Alternative measurements
	// that exist if real-cluster testing shows this mapping is wrong:
	// cq_performance_error_code (tag code) and cq_performance_error_ns
	// (tag namespace).
	measErrorHead     = "cq_performance_error_head"
	tagHead           = "head"
	fieldSystemErrors = "system_errors"
	fieldUserErrors   = "user_errors"
)

// TODO(real-device verification): no bandwidth/throughput/error unit
// conversion is applied below (raw Flux float values are passed through) -
// docs/design.md's unit-conversion callout section only mentions ms->s for
// latency and the GB/KB conversions used elsewhere; there is no documented
// unit for total_read_requests_size / total_write_requests_size /
// system_errors / user_errors, so they are assumed to already be in the
// target unit (bytes/sec, count respectively). Confirm against a live
// cluster.

// fluxDuration formats d as a Flux duration literal. time.Duration.String()
// is not used directly because it can emit "µs"/"ns" unit suffixes that
// Flux's duration-literal grammar does not recognize; this rounds to the
// nearest second and only includes the h/m/s components that are nonzero
// (always including at least "0s" if the rounded duration is zero).
func fluxDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d == 0 {
		return "0s"
	}

	var sb strings.Builder
	if d < 0 {
		sb.WriteByte('-')
		d = -d
	}

	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		fmt.Fprintf(&sb, "%dh", h)
	}
	if m > 0 {
		fmt.Fprintf(&sb, "%dm", m)
	}
	if s > 0 {
		fmt.Fprintf(&sb, "%ds", s)
	}
	return sb.String()
}

// fluxLastQuery builds a Flux query selecting the most recent value(s) of
// the given measurement over the last rangeSpan, with no tag filter, e.g.:
//
//	from(bucket:"monitoring_vdc") |> range(start: -5m) |> filter(fn: (r) => r._measurement == "cq_performance_throughput") |> last()
func fluxLastQuery(rangeSpan, measurement string) string {
	return fmt.Sprintf(
		`from(bucket:%q) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %q) |> last()`,
		perfBucket, rangeSpan, measurement,
	)
}

// fluxLastQueryWithTag builds a Flux query like fluxLastQuery, additionally
// ANDing in an exact-match filter on the given tag, e.g.:
//
//	from(bucket:"monitoring_vdc") |> range(start: -5m) |> filter(fn: (r) => r._measurement == "cq_performance_transaction_method" and r.method == "READ") |> last()
func fluxLastQueryWithTag(rangeSpan, measurement, tag, tagValue string) string {
	return fmt.Sprintf(
		`from(bucket:%q) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %q and r.%s == %q) |> last()`,
		perfBucket, rangeSpan, measurement, tag, tagValue,
	)
}

// ---------------------------------------------------------------------
// Metric descriptors, parsing and registration.
// ---------------------------------------------------------------------

var (
	perfReadLatencyDesc = prometheus.NewDesc(
		"obs_perf_read_latency_seconds",
		"Read transaction latency (p50) for the VDC, in seconds.",
		[]string{"vdc"}, nil,
	)
	perfWriteLatencyDesc = prometheus.NewDesc(
		"obs_perf_write_latency_seconds",
		"Write transaction latency (p50) for the VDC, in seconds.",
		[]string{"vdc"}, nil,
	)
	perfReadBytesDesc = prometheus.NewDesc(
		"obs_perf_read_bytes_per_second",
		"Read throughput for the VDC, in bytes per second.",
		[]string{"vdc"}, nil,
	)
	perfWriteBytesDesc = prometheus.NewDesc(
		"obs_perf_write_bytes_per_second",
		"Write throughput for the VDC, in bytes per second.",
		[]string{"vdc"}, nil,
	)
	perfReadTPSDesc = prometheus.NewDesc(
		"obs_perf_read_transactions_per_second",
		"Read transactions per second for the VDC (succeeded + failed).",
		[]string{"vdc"}, nil,
	)
	perfWriteTPSDesc = prometheus.NewDesc(
		"obs_perf_write_transactions_per_second",
		"Write transactions per second for the VDC (succeeded + failed).",
		[]string{"vdc"}, nil,
	)
	perfErrorsDesc = prometheus.NewDesc(
		"obs_perf_transaction_errors",
		"Transaction error count for the VDC by category and error type.",
		[]string{"vdc", "category", "error_type"}, nil,
	)
)

func init() {
	Registry["perf"] = collectPerf
}

// collectPerf implements Registry["perf"]. It determines the vdc label via
// run.LocalZone (memoized - falling back to "unknown" on failure, since the
// Flux data is still useful even without a clean vdc label, so this does
// not abort the scrape), then runs the 6 Flux queries described in the
// constants block above concurrently (mirroring metering.go's WaitGroup
// fan-out pattern; each query is independent of the others, so there is no
// reason to serialize them). Per docs/design.md's collector error-handling
// policy, a non-nil error is returned only if the resulting metrics slice
// ends up completely empty and at least one query itself failed
// (network/parse error, not an empty/absent result set); otherwise
// everything collected is registered and nil is returned. Each individual
// query failure is logged via Settings.Logger.Warn (best-effort: one bad
// query must not drop the others' metrics).
func collectPerf(ctx context.Context, run *Run, registry *prometheus.Registry) error {
	vdc := "unknown"
	if lz, err := run.LocalZone(ctx); err == nil {
		vdc = lz.Name
	}

	rangeSpan := fluxDuration(run.Settings.PerfRange)

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		metrics  []prometheus.Metric
		firstErr error
	)

	// runQuery executes query and returns its results; a failure is
	// recorded (first error wins, for the empty-result fallback below) and
	// logged, but never aborts the other in-flight queries.
	runQuery := func(query string) []obsclient.FluxResult {
		results, err := run.Client.Query(ctx, query)
		if err != nil {
			mu.Lock()
			if firstErr == nil {
				firstErr = err
			}
			mu.Unlock()
			if run.Settings.Logger != nil {
				run.Settings.Logger.Warn("perf: flux query failed", "query", query, "err", err)
			}
			return nil
		}
		return results
	}

	addMetrics := func(m ...prometheus.Metric) {
		mu.Lock()
		metrics = append(metrics, m...)
		mu.Unlock()
	}

	goQuery := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	goQuery(func() {
		for _, r := range runQuery(fluxLastQuery(rangeSpan, measThroughput)) {
			switch r.Field {
			case fieldReadBytes:
				addMetrics(prometheus.MustNewConstMetric(perfReadBytesDesc, prometheus.GaugeValue, r.Value, vdc))
			case fieldWriteBytes:
				addMetrics(prometheus.MustNewConstMetric(perfWriteBytesDesc, prometheus.GaugeValue, r.Value, vdc))
			}
		}
	})

	goQuery(func() {
		if v, ok := sumTPS(runQuery(fluxLastQueryWithTag(rangeSpan, measTransactionMethod, tagMethod, tagMethodRead))); ok {
			addMetrics(prometheus.MustNewConstMetric(perfReadTPSDesc, prometheus.GaugeValue, v, vdc))
		}
	})
	goQuery(func() {
		if v, ok := sumTPS(runQuery(fluxLastQueryWithTag(rangeSpan, measTransactionMethod, tagMethod, tagMethodWrite))); ok {
			addMetrics(prometheus.MustNewConstMetric(perfWriteTPSDesc, prometheus.GaugeValue, v, vdc))
		}
	})

	goQuery(func() {
		if v, ok := findField(runQuery(fluxLastQueryWithTag(rangeSpan, measLatency, tagID, tagIDRead)), fieldP50); ok {
			addMetrics(prometheus.MustNewConstMetric(perfReadLatencyDesc, prometheus.GaugeValue, v/1000, vdc))
		}
	})
	goQuery(func() {
		if v, ok := findField(runQuery(fluxLastQueryWithTag(rangeSpan, measLatency, tagID, tagIDWrite)), fieldP50); ok {
			addMetrics(prometheus.MustNewConstMetric(perfWriteLatencyDesc, prometheus.GaugeValue, v/1000, vdc))
		}
	})

	goQuery(func() {
		for _, r := range runQuery(fluxLastQuery(rangeSpan, measErrorHead)) {
			var category string
			switch r.Field {
			case fieldSystemErrors:
				category = "system"
			case fieldUserErrors:
				category = "user"
			default:
				continue
			}
			addMetrics(prometheus.MustNewConstMetric(perfErrorsDesc, prometheus.GaugeValue, r.Value, vdc, category, r.Tags[tagHead]))
		}
	})

	wg.Wait()

	if len(metrics) == 0 && firstErr != nil {
		return firstErr
	}

	registry.MustRegister(newConstCollector(metrics))
	return nil
}

// sumTPS reconstructs "requests per second" including failures (closer to
// the old dashboard field's meaning than success-only would be) by summing
// the succeed_request_counter and failed_request_counter fields of a
// method-filtered cq_performance_transaction_method query.
func sumTPS(results []obsclient.FluxResult) (float64, bool) {
	var sum float64
	found := false
	for _, r := range results {
		switch r.Field {
		case fieldSucceedCounter, fieldFailedCounter:
			sum += r.Value
			found = true
		}
	}
	return sum, found
}

// findField returns the value of the first result with the given field
// name, or (0, false) if none matched.
func findField(results []obsclient.FluxResult, field string) (float64, bool) {
	for _, r := range results {
		if r.Field == field {
			return r.Value, true
		}
	}
	return 0, false
}
