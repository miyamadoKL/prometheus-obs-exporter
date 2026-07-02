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

func TestCollectMetering(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match on r.URL.Path only, ignoring the query string, matching the
		// real client's request pattern (GetNamespaceBilling always appends
		// ?sizeunit=KB).
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/object/namespaces":
			serveFixture(t, w, "namespaces.json")
		case "/object/namespaces/namespace/ns1/quota":
			serveFixture(t, w, "namespace_quota_ns1.json")
		case "/object/namespaces/namespace/ns2/quota":
			serveFixture(t, w, "namespace_quota_ns2.json")
		case "/object/billing/namespace/ns1/info":
			if got := r.URL.Query().Get("sizeunit"); got != "KB" {
				t.Errorf("sizeunit query = %q, want KB", got)
			}
			serveFixture(t, w, "namespace_billing_ns1.json")
		case "/object/billing/namespace/ns2/info":
			if got := r.URL.Query().Get("sizeunit"); got != "KB" {
				t.Errorf("sizeunit query = %q, want KB", got)
			}
			serveFixture(t, w, "namespace_billing_ns2.json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	// Exercise the bounded-concurrency path (2 namespaces, concurrency 2).
	run := NewRun(context.Background(), c, Settings{MeteringConcurrency: 2})
	registry := prometheus.NewRegistry()

	if err := collectMetering(context.Background(), run, registry); err != nil {
		t.Fatalf("collectMetering: %v", err)
	}

	// ns1 quota: blockSize=100 GB, notificationSize=80 GB -> *1e9 bytes.
	// ns2 quota: blockSize=50 GB, notificationSize=40 GB -> *1e9 bytes.
	// ns1 billing: total_size=678901234 KB -> *1e3 bytes; total_objects=12345.
	// ns2 billing: total_size=2000 KB -> *1e3 bytes; total_objects=999.
	want := `
# HELP obs_metering_namespace_objects Number of objects in the namespace as reported by the billing API.
# TYPE obs_metering_namespace_objects gauge
obs_metering_namespace_objects{namespace="ns1"} 12345
obs_metering_namespace_objects{namespace="ns2"} 999
# HELP obs_metering_namespace_quota_bytes Configured namespace quota, in bytes, by quota type.
# TYPE obs_metering_namespace_quota_bytes gauge
obs_metering_namespace_quota_bytes{namespace="ns1",quota="block"} 1e+11
obs_metering_namespace_quota_bytes{namespace="ns1",quota="notification"} 8e+10
obs_metering_namespace_quota_bytes{namespace="ns2",quota="block"} 5e+10
obs_metering_namespace_quota_bytes{namespace="ns2",quota="notification"} 4e+10
# HELP obs_metering_namespace_used_bytes Namespace usage as reported by the billing API, in bytes.
# TYPE obs_metering_namespace_used_bytes gauge
obs_metering_namespace_used_bytes{namespace="ns1"} 6.78901234e+11
obs_metering_namespace_used_bytes{namespace="ns2"} 2e+06
`

	if err := testutil.GatherAndCompare(registry, strings.NewReader(want),
		"obs_metering_namespace_quota_bytes", "obs_metering_namespace_used_bytes", "obs_metering_namespace_objects",
	); err != nil {
		t.Fatalf("unexpected metrics:\n%v", err)
	}
}

// TestCollectMeteringSuppressesZeroBlockSizeQuota verifies that a
// namespace whose quota API reports blockSize=0 - ECS's sentinel for "no
// quota configured" (ECS minimum configurable quota is 1GB per
// dell-ecs/admin-guide/05-namespaces.md) - does not emit either
// obs_metering_namespace_quota_bytes series (block or notification), while
// the namespace's billing (used bytes / object count) metrics are still
// emitted normally.
func TestCollectMeteringSuppressesZeroBlockSizeQuota(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/object/namespaces":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"namespace":[{"name":"ns1","id":"urn:storageos:TenantOrg:ns1"}]}`))
		case "/object/namespaces/namespace/ns1/quota":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"namespace":"ns1","blockSize":"0","notificationSize":"0"}`))
		case "/object/billing/namespace/ns1/info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"namespace":"ns1","total_objects":"5","total_size":"1000"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	run := NewRun(context.Background(), c, Settings{MeteringConcurrency: 1})
	registry := prometheus.NewRegistry()

	if err := collectMetering(context.Background(), run, registry); err != nil {
		t.Fatalf("collectMetering: %v", err)
	}

	want := `
# HELP obs_metering_namespace_objects Number of objects in the namespace as reported by the billing API.
# TYPE obs_metering_namespace_objects gauge
obs_metering_namespace_objects{namespace="ns1"} 5
# HELP obs_metering_namespace_used_bytes Namespace usage as reported by the billing API, in bytes.
# TYPE obs_metering_namespace_used_bytes gauge
obs_metering_namespace_used_bytes{namespace="ns1"} 1e+06
`

	if err := testutil.GatherAndCompare(registry, strings.NewReader(want),
		"obs_metering_namespace_quota_bytes", "obs_metering_namespace_used_bytes", "obs_metering_namespace_objects",
	); err != nil {
		t.Fatalf("unexpected metrics:\n%v", err)
	}
}
