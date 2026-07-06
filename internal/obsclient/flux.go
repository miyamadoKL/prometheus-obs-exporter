package obsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// fluxQueryPath は Flux API のエンドポイント。
// docs/api-research/flux-api-reference.md に従い、常に management ポート
// 経由でアクセスする。
const fluxQueryPath = "/flux/api/external/v2/query"

// fluxMetaColumns は、tag ではなくメタデータである Flux レスポンスの
// カラム: Tags にコピーされる代わりに、専用の FluxResult フィールドへ
// 取り込まれる（"table"/"_start"/"_stop" については単に破棄される）。
var fluxMetaColumns = map[string]bool{
	"table":        true,
	"_start":       true,
	"_stop":        true,
	"_time":        true,
	"_value":       true,
	"_field":       true,
	"_measurement": true,
}

// FluxResult は Flux API のレスポンス行から抽出した、1つの平坦化された
// (measurement, field, tags, value) データポイント。意図的に汎用的な設計に
// している: Query 経由で実行される Flux クエリはどれも、対象の
// measurement/tag セットに関わらずこの型にデコードされる。これは
// perf コレクターが前提とする、docs/design.md の「クエリに依存しない
// Flux クライアント」という要件に合わせたもの。
type FluxResult struct {
	Measurement string
	Field       string
	Value       float64
	Time        time.Time
	// Tags は table/_start/_stop/_time/_value/_field/_measurement 以外の
	// 全レスポンスカラムを保持する。例: host, node_id, method, head,
	// namespace, code, id（flux-api-reference.md section 1.9, 3.2 参照）。
	Tags map[string]string
}

// Query は Flux クエリ（例:
// `from(bucket:"monitoring_vdc") |> range(start: -5m) |> filter(...) |> last()`）
// を accept/content-type を application/json にして
// POST /flux/api/external/v2/query に対して実行し、返ってきた各シリーズの
// 全行を FluxResult に平坦化する。
//
// _value セルを float64 としてパースできない行は、クエリ全体を失敗させずに
// スキップする。measurement の中には非数値のタグのみの行を含むものが
// あるため。
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
