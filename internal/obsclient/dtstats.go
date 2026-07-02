package obsclient

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// NodeDTStats is the result of probing a single node's DT statistics and
// ping endpoints. Both endpoints are plain, unauthenticated XML and are not
// documented in the ECS 3.8 / ObjectScale 4.x manuals (docs/design.md notes
// their continued existence is unconfirmed); Err is set (and the numeric
// fields left zero) if either request failed, so a single unreachable node
// does not abort the whole scrape.
type NodeDTStats struct {
	Node string

	TotalDT           float64
	UnreadyDT         float64
	UnknownDT         float64
	ActiveConnections float64

	Err error
}

// GetNodeDTStats fetches http://<node>:9101/stats/dt/DTInitStat (DT
// counters) and https://<node>:<ObjPort>/?ping (active connection count)
// for a single node and combines them into one result. It performs no
// authentication, matching the two endpoints' plaintext/anonymous nature in
// the original exporter.
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

// GetAllNodeDTStats fetches GetNodeDTStats for every node in nodes
// concurrently (mirroring the original exporter's
// RetrieveNodeStateParallel), returning one result per input node in the
// same order.
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

// getRawXML performs a plain, unauthenticated GET request and returns the
// raw response body, failing on any non-200 status.
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
