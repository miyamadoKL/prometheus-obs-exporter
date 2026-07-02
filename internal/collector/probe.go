// Package collector implements the Prometheus collectors dispatched by
// GET /probe (see docs/design.md, Phase C/D). This file defines the
// dispatch interface that cmd/obs-exporter/main.go's /probe handler is
// wired against, plus the concrete cluster/replication/node/metering/perf
// collectors (cluster.go, replication.go, node.go, metering.go, perf.go).
package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
)

// DefaultCollectors is used when the /probe request omits the collectors
// query parameter. metering is deliberately excluded (see docs/design.md:
// it scales with namespace count and must be requested explicitly).
var DefaultCollectors = []string{"cluster", "replication", "node", "perf"}

// Settings holds request-scoped collector configuration, threaded through
// every /probe invocation via Run. It replaces the package-level
// DTStatsEnabled / MeteringConcurrency / PerfRange variables that used to
// live in node.go / metering.go / perf.go: those were process-global and
// could not vary per request or be exercised safely in parallel tests.
type Settings struct {
	// DTStatsEnabled toggles the node collector's DT-stats/ping scrape
	// (docs/design.md: --collector.node.dt-stats /
	// OBS_EXPORTER_COLLECTOR_NODE_DT_STATS).
	DTStatsEnabled bool
	// MeteringConcurrency bounds how many namespaces are queried
	// concurrently by the metering collector (docs/design.md:
	// --collector.metering.concurrency).
	MeteringConcurrency int
	// PerfRange is the lookback window used for every Flux query in the
	// perf collector (docs/design.md: --collector.perf.range).
	PerfRange time.Duration
	// Logger receives collector-level and best-effort per-item failure
	// logs (docs/design.md's error-visibility requirements). May be nil,
	// in which case logging is skipped.
	Logger *slog.Logger
}

// Run bundles everything a single /probe invocation's collectors need: the
// authenticated client, the request's Settings, and memoized accessors for
// API calls that more than one collector needs (LocalZone, Nodes) so that a
// single /probe call issues each of those requests at most once even when
// multiple collectors want the same data (docs/design.md; see NewRun).
type Run struct {
	Client   *obsclient.Client
	Settings Settings

	localZone func() (*obsclient.LocalZone, error)
	nodes     func() ([]obsclient.Node, error)
}

// NewRun builds a Run for a single /probe invocation. ctx is captured for
// the lifetime of the memoized LocalZone/Nodes accessors below: every
// collector invoked for this Run is called with the same ctx (see Probe),
// so binding it once here is equivalent to threading it through each call
// while still fitting sync.OnceValues' no-argument function shape.
func NewRun(ctx context.Context, client *obsclient.Client, settings Settings) *Run {
	return &Run{
		Client:   client,
		Settings: settings,
		localZone: sync.OnceValues(func() (*obsclient.LocalZone, error) {
			return client.GetLocalZone(ctx)
		}),
		nodes: sync.OnceValues(func() ([]obsclient.Node, error) {
			return client.GetNodes(ctx)
		}),
	}
}

// LocalZone returns GET /dashboard/zones/localzone's result, fetching it at
// most once per Run regardless of how many collectors call this. ctx is
// accepted for signature symmetry/future flexibility; the underlying
// request always uses the ctx the Run was constructed with (see NewRun).
func (r *Run) LocalZone(ctx context.Context) (*obsclient.LocalZone, error) {
	return r.localZone()
}

// Nodes returns GET /vdc/nodes's result, fetching it at most once per Run
// regardless of how many collectors call this. See LocalZone for the ctx
// caveat.
func (r *Run) Nodes(ctx context.Context) ([]obsclient.Node, error) {
	return r.nodes()
}

// Func is the shape every concrete collector implementation must satisfy.
// It runs a single collector's scrape using run (the authenticated client,
// this request's Settings, and the memoized LocalZone/Nodes accessors) and
// registers its metrics directly into registry. Errors should be reserved
// for failures that mean no metrics could be produced at all; partial data
// should be emitted on a best-effort basis rather than turned into an
// error.
type Func func(ctx context.Context, run *Run, registry *prometheus.Registry) error

// Registry maps a collector name (as accepted by the /probe "collectors"
// query parameter) to its implementation. cluster.go / replication.go /
// node.go / metering.go / perf.go each populate this from their own
// init().
var Registry = map[string]Func{}

var (
	scrapeSuccessDesc = prometheus.NewDesc(
		"obs_scrape_success",
		"1 if the named collector completed successfully against this target, 0 otherwise.",
		[]string{"collector"}, nil,
	)
	scrapeDurationDesc = prometheus.NewDesc(
		"obs_scrape_duration_seconds",
		"Time in seconds the named collector took to scrape this target.",
		[]string{"collector"}, nil,
	)
)

// Probe runs each of the requested collectors (by name, as looked up in
// Registry) against client, registering their metrics into registry, along
// with a per-collector obs_scrape_success / obs_scrape_duration_seconds
// pair (docs/design.md: "collector単位で失敗を隔離し...に反映する").
//
// A single Run is constructed for the whole call (see NewRun) so that
// LocalZone/Nodes are fetched at most once even when multiple requested
// collectors need them.
//
// An unknown collector name (not present in Registry) is recorded as
// obs_scrape_success=0 and logged, rather than aborting the whole request,
// so a single bad/unimplemented collector name never fails an otherwise
// successful scrape.
//
// Probe itself never returns a non-nil error for missing/failing
// collectors; it is reserved for programmer errors (e.g. a nil client).
func Probe(ctx context.Context, client *obsclient.Client, settings Settings, collectors []string, registry *prometheus.Registry) error {
	run := NewRun(ctx, client, settings)

	successMetrics := make([]prometheus.Metric, 0, len(collectors))
	durationMetrics := make([]prometheus.Metric, 0, len(collectors))

	for _, name := range collectors {
		start := time.Now()

		success := 0.0
		if fn, ok := Registry[name]; ok {
			if err := fn(ctx, run, registry); err == nil {
				success = 1.0
			} else if settings.Logger != nil {
				settings.Logger.Error("collector failed", "collector", name, "err", err)
			}
		} else if settings.Logger != nil {
			settings.Logger.Error("unknown collector", "collector", name)
		}

		duration := time.Since(start).Seconds()
		successMetrics = append(successMetrics, prometheus.MustNewConstMetric(scrapeSuccessDesc, prometheus.GaugeValue, success, name))
		durationMetrics = append(durationMetrics, prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, duration, name))
	}

	registry.MustRegister(newConstCollector(append(successMetrics, durationMetrics...)))
	return nil
}
