package obsclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient/obsclienttest"
)

func newFixtureServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set(obsclienttest.AuthTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
			return
		}
		fixture, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		obsclienttest.ServeFixture(t, w, fixture)
	}))
}

func TestGetLocalZone(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{
		"/dashboard/zones/localzone": "localzone.json",
	})
	defer srv.Close()

	c := obsclienttest.NewTestClient(t, srv.URL, obsclienttest.Username, obsclienttest.Password)
	lz, err := c.GetLocalZone(context.Background())
	if err != nil {
		t.Fatalf("GetLocalZone: %v", err)
	}

	if lz.Name != "vdc1" {
		t.Errorf("Name = %q, want vdc1", lz.Name)
	}

	if v, err := lz.NumGoodNodes.Float64(); err != nil || v != 4 {
		t.Errorf("NumGoodNodes = %v (err=%v), want 4", v, err)
	}
	if v, err := lz.NumBadDisks.Float64(); err != nil || v != 0 {
		t.Errorf("NumBadDisks = %v (err=%v), want 0", v, err)
	}

	if v, ok := obsclient.FirstCount(lz.AlertsNumUnackCritical); !ok || v != 0 {
		t.Errorf("AlertsNumUnackCritical first = %v (ok=%v), want 0", v, ok)
	}
	if v, ok := obsclient.FirstCount(lz.AlertsNumUnackError); !ok || v != 1 {
		t.Errorf("AlertsNumUnackError first = %v (ok=%v), want 1", v, ok)
	}

	if v, ok := obsclient.FirstSpace(lz.DiskSpaceTotalCurrent); !ok || v != 40960 {
		t.Errorf("DiskSpaceTotalCurrent first = %v (ok=%v), want 40960", v, ok)
	}
	if v, ok := obsclient.FirstSpace(lz.DiskSpaceFreeCurrent); !ok || v != 10240.5 {
		t.Errorf("DiskSpaceFreeCurrent first = %v (ok=%v), want 10240.5", v, ok)
	}
}

func TestGetReplicationGroups(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{
		"/dashboard/zones/localzone/replicationgroups": "replicationgroups.json",
	})
	defer srv.Close()

	c := obsclienttest.NewTestClient(t, srv.URL, obsclienttest.Username, obsclienttest.Password)
	groups, err := c.GetReplicationGroups(context.Background())
	if err != nil {
		t.Fatalf("GetReplicationGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}

	rg1 := groups[0]
	if rg1.Name != "rg1" {
		t.Errorf("groups[0].Name = %q, want rg1", rg1.Name)
	}
	if v, err := rg1.ReplicationIngressTraffic.Float64(); err != nil || v != 1024.0 {
		t.Errorf("rg1 ReplicationIngressTraffic = %v (err=%v), want 1024.0", v, err)
	}
	if v, err := rg1.ChunksJournalPendingReplicationTotalSize.Float64(); err != nil || v != 512 {
		t.Errorf("rg1 ChunksJournalPendingReplicationTotalSize = %v (err=%v), want 512", v, err)
	}

	rg2 := groups[1]
	if v, err := rg2.ReplicationRpoTimestamp.Int64(); err != nil || v != 1751500001000 {
		t.Errorf("rg2 ReplicationRpoTimestamp = %v (err=%v), want 1751500001000", v, err)
	}
}

func TestGetNodes(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{
		"/vdc/nodes": "nodes.json",
	})
	defer srv.Close()

	c := obsclienttest.NewTestClient(t, srv.URL, obsclienttest.Username, obsclienttest.Password)
	nodes, err := c.GetNodes(context.Background())
	if err != nil {
		t.Fatalf("GetNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2", len(nodes))
	}
	if nodes[0].MgmtIP != "10.0.1.1" || nodes[0].DataIP != "10.0.0.1" {
		t.Errorf("nodes[0] = %+v, unexpected IPs", nodes[0])
	}
	if nodes[0].Version != "3.8.1.2" {
		t.Errorf("nodes[0].Version = %q, want 3.8.1.2", nodes[0].Version)
	}
}
