package collector

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestNodeDTMetrics unit-tests nodeDTMetrics directly by hand-constructing
// input structs, rather than going through collectNode against an
// httptest server: obsclient hardcodes port :9101 for DT stats, so there is
// no way to redirect that call to a test server.
func TestNodeDTMetrics(t *testing.T) {
	stats := []obsclient.NodeDTStats{
		{
			Node:              "10.0.1.1",
			TotalDT:           100,
			UnreadyDT:         2,
			UnknownDT:         1,
			ActiveConnections: 50,
		},
		{
			// Err set: must be excluded entirely from the output, even
			// though it sits between two otherwise-valid entries.
			Node: "10.0.1.2",
			Err:  errors.New("unreachable"),
		},
		{
			Node:              "10.0.1.3",
			TotalDT:           200,
			UnreadyDT:         0,
			UnknownDT:         0,
			ActiveConnections: 10,
		},
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(newConstCollector(nodeDTMetrics(stats)))

	want := `
# HELP obs_node_active_connections Active connection count reported by the node's ping endpoint.
# TYPE obs_node_active_connections gauge
obs_node_active_connections{node="10.0.1.1"} 50
obs_node_active_connections{node="10.0.1.3"} 10
# HELP obs_node_directory_tables Total number of directory tables (DTs) reported by the node.
# TYPE obs_node_directory_tables gauge
obs_node_directory_tables{node="10.0.1.1"} 100
obs_node_directory_tables{node="10.0.1.3"} 200
# HELP obs_node_directory_tables_unknown Number of unknown directory tables (DTs) reported by the node.
# TYPE obs_node_directory_tables_unknown gauge
obs_node_directory_tables_unknown{node="10.0.1.1"} 1
obs_node_directory_tables_unknown{node="10.0.1.3"} 0
# HELP obs_node_directory_tables_unready Number of unready directory tables (DTs) reported by the node.
# TYPE obs_node_directory_tables_unready gauge
obs_node_directory_tables_unready{node="10.0.1.1"} 2
obs_node_directory_tables_unready{node="10.0.1.3"} 0
`

	if err := testutil.GatherAndCompare(registry, strings.NewReader(want),
		"obs_node_directory_tables", "obs_node_directory_tables_unready", "obs_node_directory_tables_unknown", "obs_node_active_connections",
	); err != nil {
		t.Fatalf("unexpected metrics:\n%v", err)
	}
}

// TestNodeDTMetricsAllErrored verifies an all-errored input produces no
// metrics at all (rather than panicking or emitting zero-valued metrics).
func TestNodeDTMetricsAllErrored(t *testing.T) {
	stats := []obsclient.NodeDTStats{
		{Node: "10.0.1.1", Err: errors.New("unreachable")},
	}
	if got := nodeDTMetrics(stats); len(got) != 0 {
		t.Fatalf("nodeDTMetrics = %d metrics, want 0", len(got))
	}
}

// TestCollectNodeDisabled verifies collectNode is a deliberate no-op
// (success, no metrics) when Settings.DTStatsEnabled is false.
func TestCollectNodeDisabled(t *testing.T) {
	ctx := context.Background()
	// client is nil: collectNode must return before touching run.Client or
	// run.Nodes when DTStatsEnabled is false.
	run := NewRun(ctx, nil, Settings{DTStatsEnabled: false})

	registry := prometheus.NewRegistry()
	if err := collectNode(ctx, run, registry); err != nil {
		t.Fatalf("collectNode: %v", err)
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(families) != 0 {
		t.Fatalf("families = %d, want 0", len(families))
	}
}
