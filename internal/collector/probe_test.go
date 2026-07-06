package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// TestProbeMemoizesLocalZoneAndNodes verifies that a single Probe call
// fetches GET /dashboard/zones/localzone and GET /vdc/nodes at most once
// each, even though cluster, node and perf all want that data (via Run's
// memoized LocalZone/Nodes accessors - see probe.go's NewRun). Before this,
// each collector called client.GetLocalZone/client.GetNodes directly,
// duplicating those requests on every /probe call that combined multiple
// collectors.
func TestProbeMemoizesLocalZoneAndNodes(t *testing.T) {
	var localZoneCalls, nodesCalls int32

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/dashboard/zones/localzone":
			atomic.AddInt32(&localZoneCalls, 1)
			serveFixture(t, w, "localzone.json")
		case "/vdc/nodes":
			atomic.AddInt32(&nodesCalls, 1)
			// mgmt_ip is loopback so the node collector's unauthenticated
			// DT-stats/ping calls (real network I/O to :9101 and the
			// configured object port, neither of which this fixture server
			// answers) fail immediately with connection-refused instead of
			// hanging on a dial timeout to an unreachable address.
			serveFixture(t, w, "nodes_loopback.json")
		case "/dashboard/zones/localzone/replicationgroups":
			serveFixture(t, w, "replicationgroups.json")
		case fluxQueryPath:
			// Empty series: perf's 6 queries all succeed trivially.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Series":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	registry := prometheus.NewRegistry()
	settings := Settings{
		DTStatsEnabled:      true,
		MeteringConcurrency: 4,
		PerfRange:           5 * time.Minute,
	}

	if err := Probe(context.Background(), c, settings, []string{"cluster", "replication", "node", "perf"}, registry); err != nil {
		t.Fatalf("Probe: %v", err)
	}

	if got := atomic.LoadInt32(&localZoneCalls); got != 1 {
		t.Errorf("GET /dashboard/zones/localzone calls = %d, want 1 (cluster and perf both need it)", got)
	}
	if got := atomic.LoadInt32(&nodesCalls); got != 1 {
		t.Errorf("GET /vdc/nodes calls = %d, want 1 (cluster and node both need it)", got)
	}
}

// TestProbeLogsUnknownCollector verifies an unrecognized collector name is
// recorded as obs_scrape_success=0 rather than aborting the request.
func TestProbeLogsUnknownCollector(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	registry := prometheus.NewRegistry()

	if err := Probe(context.Background(), c, Settings{}, []string{"no-such-collector"}, registry); err != nil {
		t.Fatalf("Probe: %v", err)
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var found bool
	for _, mf := range families {
		if mf.GetName() != "obs_scrape_success" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "collector" && l.GetValue() == "no-such-collector" {
					found = true
					if m.GetGauge().GetValue() != 0 {
						t.Errorf("obs_scrape_success for unknown collector = %v, want 0", m.GetGauge().GetValue())
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("obs_scrape_success{collector=\"no-such-collector\"} not found")
	}
}
