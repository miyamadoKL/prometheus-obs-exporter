package obsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// fluxQueryPath is the Flux API endpoint, always reached over the
// management port per docs/api-research/flux-api-reference.md.
const fluxQueryPath = "/flux/api/external/v2/query"

// fluxMetaColumns are Flux response columns that are metadata rather than
// tags: they are consumed into dedicated FluxResult fields (or, for
// "table"/"_start"/"_stop", dropped) instead of being copied into Tags.
var fluxMetaColumns = map[string]bool{
	"table":        true,
	"_start":       true,
	"_stop":        true,
	"_time":        true,
	"_value":       true,
	"_field":       true,
	"_measurement": true,
}

// FluxResult is one flattened (measurement, field, tags, value) data point
// extracted from a Flux API response row. It is intentionally generic: any
// Flux query executed via Query decodes into these regardless of which
// measurement/tag set it targets, matching docs/design.md's requirement of
// a query-agnostic Flux client that the perf collector builds on.
type FluxResult struct {
	Measurement string
	Field       string
	Value       float64
	Time        time.Time
	// Tags holds every response column other than table/_start/_stop/_time/
	// _value/_field/_measurement, e.g. host, node_id, method, head,
	// namespace, code, id (see flux-api-reference.md section 1.9 and 3.2).
	Tags map[string]string
}

// Query executes a Flux query (e.g.
// `from(bucket:"monitoring_vdc") |> range(start: -5m) |> filter(...) |> last()`)
// against POST /flux/api/external/v2/query with accept/content-type
// application/json, and flattens every row of every returned series into a
// FluxResult.
//
// Rows whose _value cell cannot be parsed as a float64 are skipped rather
// than failing the whole query, since some measurements carry non-numeric
// tag-only rows.
func (c *Client) Query(ctx context.Context, query string) ([]FluxResult, error) {
	reqBody, err := json.Marshal(fluxQueryRequest{Query: query})
	if err != nil {
		return nil, fmt.Errorf("obsclient: encoding flux query: %w", err)
	}

	var resp FluxQueryResponse
	if err := c.postAuthenticatedJSON(ctx, fluxQueryPath, reqBody, &resp); err != nil {
		return nil, fmt.Errorf("obsclient: flux query: %w", err)
	}

	var results []FluxResult
	for _, series := range resp.Series {
		results = append(results, flattenFluxSeries(series)...)
	}
	return results, nil
}

func flattenFluxSeries(s FluxSeries) []FluxResult {
	colIndex := make(map[string]int, len(s.Columns))
	for i, col := range s.Columns {
		colIndex[col] = i
	}

	valueIdx, ok := colIndex["_value"]
	if !ok {
		return nil
	}

	results := make([]FluxResult, 0, len(s.Values))
	for _, row := range s.Values {
		if valueIdx >= len(row) {
			continue
		}
		value, err := strconv.ParseFloat(row[valueIdx], 64)
		if err != nil {
			continue
		}

		res := FluxResult{Value: value, Tags: map[string]string{}}
		if i, ok := colIndex["_measurement"]; ok && i < len(row) {
			res.Measurement = row[i]
		}
		if i, ok := colIndex["_field"]; ok && i < len(row) {
			res.Field = row[i]
		}
		if i, ok := colIndex["_time"]; ok && i < len(row) {
			if t, err := time.Parse(time.RFC3339, row[i]); err == nil {
				res.Time = t
			}
		}
		for col, i := range colIndex {
			if fluxMetaColumns[col] || i >= len(row) {
				continue
			}
			res.Tags[col] = row[i]
		}

		results = append(results, res)
	}
	return results
}
