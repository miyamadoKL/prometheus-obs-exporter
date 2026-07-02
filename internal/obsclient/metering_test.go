package obsclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient/obsclienttest"
)

// TestMeteringUsesConfiguredMgmtPort exercises namespaces/quota/billing
// against a server bound to a random (non-4443) port. The original
// exporter hardcoded :4443 for these three calls; if that regressed, every
// request below would fail to connect since NewTestClient only points the
// client at the test server's actual (random) port.
func TestMeteringUsesConfiguredMgmtPort(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(obsclienttest.AuthTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/object/namespaces":
			obsclienttest.ServeFixture(t, w, "namespaces.json")
		case "/object/namespaces/namespace/ns1/quota":
			obsclienttest.ServeFixture(t, w, "namespace_quota.json")
		case "/object/billing/namespace/ns1/info":
			if r.URL.Query().Get("sizeunit") != "KB" {
				t.Errorf("sizeunit query = %q, want KB", r.URL.Query().Get("sizeunit"))
			}
			obsclienttest.ServeFixture(t, w, "namespace_billing.json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := obsclienttest.NewTestClient(t, srv.URL, obsclienttest.Username, obsclienttest.Password)
	ctx := context.Background()

	namespaces, err := c.ListNamespaces(ctx)
	if err != nil {
		t.Fatalf("ListNamespaces: %v", err)
	}
	if len(namespaces) != 2 || namespaces[0].Name != "ns1" {
		t.Fatalf("namespaces = %+v, unexpected", namespaces)
	}

	quota, err := c.GetNamespaceQuota(ctx, "ns1")
	if err != nil {
		t.Fatalf("GetNamespaceQuota: %v", err)
	}
	if v, err := quota.BlockSize.Float64(); err != nil || v != 100 {
		t.Errorf("BlockSize = %v (err=%v), want 100", v, err)
	}
	if v, err := quota.NotificationSize.Float64(); err != nil || v != 80 {
		t.Errorf("NotificationSize = %v (err=%v), want 80", v, err)
	}

	billing, err := c.GetNamespaceBilling(ctx, "ns1")
	if err != nil {
		t.Fatalf("GetNamespaceBilling: %v", err)
	}
	if v, err := billing.TotalObjects.Int64(); err != nil || v != 12345 {
		t.Errorf("TotalObjects = %v (err=%v), want 12345", v, err)
	}
	if v, err := billing.TotalSize.Float64(); err != nil || v != 678901234 {
		t.Errorf("TotalSize = %v (err=%v), want 678901234", v, err)
	}
}

// TestGetNamespaceQuotaEscapesNamespace verifies namespace names are
// path-escaped when building the quota/billing URLs.
func TestGetNamespaceQuotaEscapesNamespace(t *testing.T) {
	const ns = "ns with space"

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(obsclienttest.AuthTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/object/namespaces/namespace/ns with space/quota":
			obsclienttest.ServeFixture(t, w, "namespace_quota.json")
		default:
			t.Errorf("unexpected path %q", r.URL.EscapedPath())
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := obsclienttest.NewTestClient(t, srv.URL, obsclienttest.Username, obsclienttest.Password)
	if _, err := c.GetNamespaceQuota(context.Background(), ns); err != nil {
		t.Fatalf("GetNamespaceQuota: %v", err)
	}
}
