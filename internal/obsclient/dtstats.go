package obsclient

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// NodeDTStats は単一ノードの DT statistics と ping エンドポイントを
// プローブした結果。どちらのエンドポイントも素の、未認証の XML であり、
// ECS 3.8 / ObjectScale 4.x のマニュアルには文書化されていない
// （docs/design.md はこれらが今も存在し続けているかは未確認と注記している）。
// どちらかのリクエストが失敗した場合は Err がセットされ（数値フィールドは
// 0のまま）、これにより到達不能な1ノードがスクレイプ全体を中断させない。
type NodeDTStats struct {
	Node string

	TotalDT           float64
	UnreadyDT         float64
	UnknownDT         float64
	ActiveConnections float64

	Err error
}

// GetNodeDTStats は単一ノードについて http://<node>:9101/stats/dt/DTInitStat
// （DT カウンタ）と https://<node>:<ObjPort>/?ping（アクティブ接続数）を
// 取得し、1つの結果にまとめる。認証は一切行わない。これは元の exporter に
// おけるこの2つのエンドポイントの平文・匿名の性質に合わせている。
func (c *Client) GetNodeDTStats(ctx context.Context, node string) NodeDTStats {
	result := NodeDTStats{Node: node}

	dtStat, err := c.getDTInitStat(ctx, node)
	if err != nil {
		result.Err = fmt.Errorf("obsclient: dt stats for node %s: %w", node, err)
		return result
	}
	result.TotalDT = dtStat.TotalDTNum
	result.UnreadyDT = dtStat.UnreadyDTNum
	result.UnknownDT = dtStat.UnknownDTNum

	ping, err := c.getPing(ctx, node)
	if err != nil {
		result.Err = fmt.Errorf("obsclient: ping for node %s: %w", node, err)
		return result
	}
	result.ActiveConnections = ping.Value

	return result
}

// GetAllNodeDTStats は nodes の全ノードについて GetNodeDTStats を並行に
// 取得し（元の exporter の RetrieveNodeStateParallel を踏襲）、入力と
// 同じ順序で1ノードにつき1結果を返す。
func (c *Client) GetAllNodeDTStats(ctx context.Context, nodes []string) []NodeDTStats {
	results := make([]NodeDTStats, len(nodes))

	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func(i int, node string) {
			defer wg.Done()
			results[i] = c.GetNodeDTStats(ctx, node)
		}(i, node)
	}
	wg.Wait()

	return results
}

func (c *Client) getDTInitStat(ctx context.Context, node string) (*DTInitStat, error) {
	url := fmt.Sprintf("http://%s:9101/stats/dt/DTInitStat", node)

	body, err := c.getRawXML(ctx, url)
	if err != nil {
		return nil, err
	}

	var stat DTInitStat
	if err := xml.Unmarshal(body, &stat); err != nil {
		return nil, fmt.Errorf("decoding XML from %s: %w", url, err)
	}
	return &stat, nil
}

func (c *Client) getPing(ctx context.Context, node string) (*PingResponse, error) {
	url := fmt.Sprintf("https://%s:%d/?ping", node, c.cfg.ObjPort)

	body, err := c.getRawXML(ctx, url)
	if err != nil {
		return nil, err
	}

	var ping PingResponse
	if err := xml.Unmarshal(body, &ping); err != nil {
		return nil, fmt.Errorf("decoding XML from %s: %w", url, err)
	}
	return &ping, nil
}

// getRawXML は素の、未認証の GET リクエストを実行し、生のレスポンス body を
// 返す。200 以外のステータスは失敗として扱う。
func (c *Client) getRawXML(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request to %s: %w", url, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	return body, nil
}
