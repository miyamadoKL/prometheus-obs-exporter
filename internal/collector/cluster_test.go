package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCollectCluster(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/dashboard/zones/localzone":
			serveFixture(t, w, "localzone.json")
		case "/vdc/nodes":
			serveFixture(t, w, "nodes.json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	run := NewRun(context.Background(), c, Settings{})
	registry := prometheus.NewRegistry()

	if err := collectCluster(context.Background(), run, registry); err != nil {
		t.Fatalf("collectCluster: %v", err)
	}

	want := `
# HELP obs_cluster_alerts_unacknowledged Number of unacknowledged alerts in the VDC by severity.
# TYPE obs_cluster_alerts_unacknowledged gauge
obs_cluster_alerts_unacknowledged{severity="critical",vdc="vdc1"} 0
obs_cluster_alerts_unacknowledged{severity="error",vdc="vdc1"} 1
obs_cluster_alerts_unacknowledged{severity="info",vdc="vdc1"} 3
obs_cluster_alerts_unacknowledged{severity="warning",vdc="vdc1"} 2
# HELP obs_cluster_capacity_free_bytes Free raw disk capacity of the VDC, in bytes.
# TYPE obs_cluster_capacity_free_bytes gauge
obs_cluster_capacity_free_bytes{vdc="vdc1"} 1.02405e+13
# HELP obs_cluster_capacity_total_bytes Total raw disk capacity of the VDC, in bytes.
# TYPE obs_cluster_capacity_total_bytes gauge
obs_cluster_capacity_total_bytes{vdc="vdc1"} 4.096e+13
# HELP obs_cluster_disks Number of disks in the VDC by health state.
# TYPE obs_cluster_disks gauge
obs_cluster_disks{state="bad",vdc="vdc1"} 0
obs_cluster_disks{state="good",vdc="vdc1"} 48
# HELP obs_cluster_info Constant 1, labeled with the cluster software version and inferred product.
# TYPE obs_cluster_info gauge
obs_cluster_info{product="ecs",vdc="vdc1",version="3.8.1.2"} 1
# HELP obs_cluster_nodes Number of nodes in the VDC by health state.
# TYPE obs_cluster_nodes gauge
obs_cluster_nodes{state="bad",vdc="vdc1"} 0
obs_cluster_nodes{state="good",vdc="vdc1"} 4
`

	if err := testutil.GatherAndCompare(registry, strings.NewReader(want),
		"obs_cluster_nodes", "obs_cluster_disks",
		"obs_cluster_capacity_total_bytes", "obs_cluster_capacity_free_bytes",
		"obs_cluster_alerts_unacknowledged", "obs_cluster_info",
	); err != nil {
		t.Fatalf("unexpected metrics:\n%v", err)
	}
}

func TestCollectClusterNodesFailureUsesUnknownVersion(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/dashboard/zones/localzone":
			serveFixture(t, w, "localzone.json")
		default:
			// /vdc/nodes deliberately not served, to exercise the best-effort
			// fallback to version="unknown"/product="unknown".
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	run := NewRun(context.Background(), c, Settings{})
	registry := prometheus.NewRegistry()

	if err := collectCluster(context.Background(), run, registry); err != nil {
		t.Fatalf("collectCluster: %v", err)
	}

	want := `
# HELP obs_cluster_info Constant 1, labeled with the cluster software version and inferred product.
# TYPE obs_cluster_info gauge
obs_cluster_info{product="unknown",vdc="vdc1",version="unknown"} 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want), "obs_cluster_info"); err != nil {
		t.Fatalf("unexpected metrics:\n%v", err)
	}
}

// TestCollectClusterMissingNumGoodNodesOmitsSeries verifies that a missing
// (empty-string) numGoodNodes field - which FlexNumber.Float64 now surfaces
// as obsclient.ErrMissing rather than a fake 0 - results in the
// obs_cluster_nodes{state="good"} series simply not being emitted, rather
// than an emitted zero. The state="bad" series (numBadNodes is present, 0)
// is still emitted normally, confirming the fake-zero fix is targeted at
// genuinely-missing fields only.
func TestCollectClusterMissingNumGoodNodesOmitsSeries(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/dashboard/zones/localzone":
			serveFixture(t, w, "localzone_missing_numgoodnodes.json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	run := NewRun(context.Background(), c, Settings{})
	registry := prometheus.NewRegistry()

	if err := collectCluster(context.Background(), run, registry); err != nil {
		t.Fatalf("collectCluster: %v", err)
	}

	want := `
# HELP obs_cluster_nodes Number of nodes in the VDC by health state.
# TYPE obs_cluster_nodes gauge
obs_cluster_nodes{state="bad",vdc="vdc1"} 0
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want), "obs_cluster_nodes"); err != nil {
		t.Fatalf("unexpected metrics:\n%v", err)
	}
}

func TestInferProduct(t *testing.T) {
	cases := map[string]string{
		"3.8.1.2": "ecs",
		"4.1.0.3": "objectscale",
		"":        "unknown",
		"5.0.0.0": "unknown",
	}
	for version, want := range cases {
		if got := inferProduct(version); got != want {
			t.Errorf("inferProduct(%q) = %q, want %q", version, got, want)
		}
	}
}
