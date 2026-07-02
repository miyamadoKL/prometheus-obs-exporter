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

func TestCollectReplication(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/dashboard/zones/localzone/replicationgroups":
			serveFixture(t, w, "replicationgroups.json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	run := NewRun(context.Background(), c, Settings{})
	registry := prometheus.NewRegistry()

	if err := collectReplication(context.Background(), run, registry); err != nil {
		t.Fatalf("collectReplication: %v", err)
	}

	// replicationgroups.json fixture: rg1.replicationRpoTimestamp =
	// "1751500000000" (epoch ms) -> 1751500000 epoch seconds; rg2's =
	// 1751500001000 (epoch ms) -> 1751500001 epoch seconds. This explicitly
	// asserts the ms->s division arithmetic.
	want := `
# HELP obs_replication_egress_bytes_per_second Replication egress traffic for the replication group, in bytes per second.
# TYPE obs_replication_egress_bytes_per_second gauge
obs_replication_egress_bytes_per_second{rg="rg1"} 2048
obs_replication_egress_bytes_per_second{rg="rg2"} 0
# HELP obs_replication_ingress_bytes_per_second Replication ingress traffic for the replication group, in bytes per second.
# TYPE obs_replication_ingress_bytes_per_second gauge
obs_replication_ingress_bytes_per_second{rg="rg1"} 1024
obs_replication_ingress_bytes_per_second{rg="rg2"} 0
# HELP obs_replication_pending_journal_bytes Journal chunks pending replication for the replication group, in bytes.
# TYPE obs_replication_pending_journal_bytes gauge
obs_replication_pending_journal_bytes{rg="rg1"} 512
obs_replication_pending_journal_bytes{rg="rg2"} 0
# HELP obs_replication_pending_repo_bytes Repo chunks pending replication for the replication group, in bytes.
# TYPE obs_replication_pending_repo_bytes gauge
obs_replication_pending_repo_bytes{rg="rg1"} 0
obs_replication_pending_repo_bytes{rg="rg2"} 128
# HELP obs_replication_pending_xor_bytes XOR chunks pending replication for the replication group, in bytes.
# TYPE obs_replication_pending_xor_bytes gauge
obs_replication_pending_xor_bytes{rg="rg1"} 0
obs_replication_pending_xor_bytes{rg="rg2"} 0
# HELP obs_replication_rpo_timestamp_seconds Recovery point objective timestamp for the replication group, as epoch seconds.
# TYPE obs_replication_rpo_timestamp_seconds gauge
obs_replication_rpo_timestamp_seconds{rg="rg1"} 1.75150000e+09
obs_replication_rpo_timestamp_seconds{rg="rg2"} 1.751500001e+09
`

	if err := testutil.GatherAndCompare(registry, strings.NewReader(want),
		"obs_replication_ingress_bytes_per_second", "obs_replication_egress_bytes_per_second",
		"obs_replication_pending_repo_bytes", "obs_replication_pending_journal_bytes",
		"obs_replication_pending_xor_bytes", "obs_replication_rpo_timestamp_seconds",
	); err != nil {
		t.Fatalf("unexpected metrics:\n%v", err)
	}
}
