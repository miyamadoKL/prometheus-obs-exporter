package obsclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFluxQueryRoundTrip(t *testing.T) {
	const query = `from(bucket:"monitoring_main") |> range(start: -30m) |> filter(fn: (r) => r._measurement == "statDataHead_performance_internal_transactions")`

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case fluxQueryPath:
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", r.Method)
			}
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Errorf("Accept header = %q, want application/json", got)
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type header = %q, want application/json", got)
			}
			if got := r.Header.Get(authTokenHeader); got != "token-1" {
				t.Errorf("auth token header = %q, want token-1", got)
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("reading request body: %v", err)
			}
			var decoded struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("decoding request body: %v", err)
			}
			if decoded.Query != query {
				t.Errorf("query = %q, want %q", decoded.Query, query)
			}

			serveFixture(t, w, "flux_query_response.json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)

	results, err := c.Query(context.Background(), query)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	r := results[0]
	if r.Measurement != "statDataHead_performance_internal_transactions" {
		t.Errorf("Measurement = %q, unexpected", r.Measurement)
	}
	if r.Field != "failed_request_counter" {
		t.Errorf("Field = %q, unexpected", r.Field)
	}
	if r.Value != 1 {
		t.Errorf("Value = %v, want 1", r.Value)
	}
	if r.Time.IsZero() {
		t.Error("Time is zero, want parsed timestamp")
	}

	wantTags := map[string]string{
		"host":    "ecs.lss.emc.com",
		"node_id": "28cd473e-ca45-4623-b30d-0481c548a650",
		"process": "statDataHead",
		"tag":     "dashboard",
	}
	for k, want := range wantTags {
		if got := r.Tags[k]; got != want {
			t.Errorf("Tags[%q] = %q, want %q", k, got, want)
		}
	}
	// Meta columns must not leak into Tags.
	for _, meta := range []string{"table", "_start", "_stop", "_time", "_value", "_field", "_measurement"} {
		if _, ok := r.Tags[meta]; ok {
			t.Errorf("Tags unexpectedly contains meta column %q", meta)
		}
	}
}

func TestFluxQuerySkipsNonNumericValues(t *testing.T) {
	resp := FluxQueryResponse{
		Series: []FluxSeries{
			{
				Columns: []string{"_value", "_field"},
				Values: [][]string{
					{"not-a-number", "some_field"},
					{"42", "other_field"},
				},
			},
		},
	}

	var results []FluxResult
	for _, s := range resp.Series {
		results = append(results, flattenFluxSeries(s)...)
	}

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Value != 42 {
		t.Errorf("Value = %v, want 42", results[0].Value)
	}
	if results[0].Field != "other_field" {
		t.Errorf("Field = %q, want other_field", results[0].Field)
	}
}
