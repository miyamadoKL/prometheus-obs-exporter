package config_test

import (
	"testing"

	"github.com/alecthomas/kingpin/v2"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/config"
)

// TestPerfRangeOverOneHourRejected verifies --collector.perf.range beyond
// the Flux API's documented 1h cap is rejected at startup (config.New's
// app.Validate hook), rather than being accepted and failing every
// perf-collector scrape at request time.
func TestPerfRangeOverOneHourRejected(t *testing.T) {
	app := kingpin.New("obs-exporter", "")
	app.Terminate(func(int) {})
	cfg := config.New(app)

	_, err := app.Parse([]string{
		"--ecs.username=u", "--ecs.password=p", "--collector.perf.range=90m",
	})
	if err == nil {
		t.Fatalf("Parse succeeded with a 90m perf range, want a validation error; cfg = %+v", cfg)
	}
}

// TestPerfRangeAtOneHourAccepted verifies exactly 1h (the documented cap)
// is still accepted.
func TestPerfRangeAtOneHourAccepted(t *testing.T) {
	app := kingpin.New("obs-exporter", "")
	app.Terminate(func(int) {})
	cfg := config.New(app)

	if _, err := app.Parse([]string{
		"--ecs.username=u", "--ecs.password=p", "--collector.perf.range=1h",
	}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.CollectorPerfRange.String() != "1h0m0s" {
		t.Errorf("CollectorPerfRange = %v, want 1h0m0s", cfg.CollectorPerfRange)
	}
}
