package collector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fluxQueryPath mirrors obsclient's unexported constant of the same name
// (internal/obsclient/flux.go); duplicated here since it cannot be
// imported.
const fluxQueryPath = "/flux/api/external/v2/query"

func TestCollectPerf(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/dashboard/zones/localzone":
			serveFixture(t, w, "localzone.json")
		case fluxQueryPath:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("reading flux query body: %v", err)
			}
			var decoded struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("decoding flux query body: %v", err)
			}
			q := decoded.Query

			switch {
			case strings.Contains(q, "cq_performance_throughput"):
				serveFixture(t, w, "perf_throughput.json")
			case strings.Contains(q, "cq_performance_transaction_method") && strings.Contains(q, `r.method == "READ"`):
				serveFixture(t, w, "perf_transaction_method_read.json")
			case strings.Contains(q, "cq_performance_transaction_method") && strings.Contains(q, `r.method == "WRITE"`):
				serveFixture(t, w, "perf_transaction_method_write.json")
			case strings.Contains(q, "cq_performance_latency") && strings.Contains(q, `r.id == "read"`):
				serveFixture(t, w, "perf_latency_read.json")
			case strings.Contains(q, "cq_performance_latency") && strings.Contains(q, `r.id == "write"`):
				serveFixture(t, w, "perf_latency_write.json")
			case strings.Contains(q, "cq_performance_error_head"):
				serveFixture(t, w, "perf_error_head.json")
			default:
				t.Errorf("unexpected flux query: %s", q)
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	run := NewRun(context.Background(), c, Settings{PerfRange: 5 * time.Minute})
	registry := prometheus.NewRegistry()

	if err := collectPerf(context.Background(), run, registry); err != nil {
		t.Fatalf("collectPerf: %v", err)
	}

	// perf_latency_{read,write}.json's p50 fields are in ms (15 / 25); the
	// collector must divide by 1000 to get seconds (0.015 / 0.025).
	// perf_transaction_method_{read,write}.json's succeed/failed counters
	// (100+5, 50+2) must be summed into TPS (105, 52).
	want := `
# HELP obs_perf_read_bytes_per_second Read throughput for the VDC, in bytes per second.
# TYPE obs_perf_read_bytes_per_second gauge
obs_perf_read_bytes_per_second{vdc="vdc1"} 123.5
# HELP obs_perf_read_latency_seconds Read transaction latency (p50) for the VDC, in seconds.
# TYPE obs_perf_read_latency_seconds gauge
obs_perf_read_latency_seconds{vdc="vdc1"} 0.015
# HELP obs_perf_read_transactions_per_second Read transactions per second for the VDC (succeeded + failed).
# TYPE obs_perf_read_transactions_per_second gauge
obs_perf_read_transactions_per_second{vdc="vdc1"} 105
# HELP obs_perf_transaction_errors Transaction error count for the VDC by category and error type.
# TYPE obs_perf_transaction_errors gauge
obs_perf_transaction_errors{category="system",error_type="S3",vdc="vdc1"} 3
obs_perf_transaction_errors{category="user",error_type="S3",vdc="vdc1"} 7
# HELP obs_perf_write_bytes_per_second Write throughput for the VDC, in bytes per second.
# TYPE obs_perf_write_bytes_per_second gauge
obs_perf_write_bytes_per_second{vdc="vdc1"} 67.25
# HELP obs_perf_write_latency_seconds Write transaction latency (p50) for the VDC, in seconds.
# TYPE obs_perf_write_latency_seconds gauge
obs_perf_write_latency_seconds{vdc="vdc1"} 0.025
# HELP obs_perf_write_transactions_per_second Write transactions per second for the VDC (succeeded + failed).
# TYPE obs_perf_write_transactions_per_second gauge
obs_perf_write_transactions_per_second{vdc="vdc1"} 52
`

	if err := testutil.GatherAndCompare(registry, strings.NewReader(want),
		"obs_perf_read_latency_seconds", "obs_perf_write_latency_seconds",
		"obs_perf_read_bytes_per_second", "obs_perf_write_bytes_per_second",
		"obs_perf_read_transactions_per_second", "obs_perf_write_transactions_per_second",
		"obs_perf_transaction_errors",
	); err != nil {
		t.Fatalf("unexpected metrics:\n%v", err)
	}
}

func TestFluxDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Minute, "5m"},
		{1 * time.Hour, "1h"},
		{90 * time.Second, "1m30s"},
		{0, "0s"},
	}
	for _, tc := range cases {
		if got := fluxDuration(tc.d); got != tc.want {
			t.Errorf("fluxDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
