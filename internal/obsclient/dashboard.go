package obsclient

import (
	"context"
	"fmt"
)

// GetLocalZone は GET /dashboard/zones/localzone を取得する。これは
// ローカル VDC の現在のノード/ディスクの健全性カウント、未確認アラート数、
// 容量を報告する。
func (c *Client) GetLocalZone(ctx context.Context) (*LocalZone, error) {
	var lz LocalZone
	if err := c.getAuthenticatedJSON(ctx, "/dashboard/zones/localzone", &lz); err != nil {
		return nil, fmt.Errorf("obsclient: get local zone: %w", err)
	}
	return &lz, nil
}

// GetReplicationGroups は
// GET /dashboard/zones/localzone/replicationgroups を取得する。これは
// レプリケーショングループごとの現在のレプリケーションスループットと
// バックログを報告する。
func (c *Client) GetReplicationGroups(ctx context.Context) ([]ReplicationGroup, error) {
	var resp ReplicationGroupsResponse
	if err := c.getAuthenticatedJSON(ctx, "/dashboard/zones/localzone/replicationgroups", &resp); err != nil {
		return nil, fmt.Errorf("obsclient: get replication groups: %w", err)
	}
	return resp.Replicationgroup, nil
}

// GetNodes は GET /vdc/nodes を取得する。これは VDC 内の全ノードを、その
// management/data IP とクラスタソフトウェアのバージョンとともに一覧する。
func (c *Client) GetNodes(ctx context.Context) ([]Node, error) {
	var resp NodesResponse
	if err := c.getAuthenticatedJSON(ctx, "/vdc/nodes", &resp); err != nil {
		return nil, fmt.Errorf("obsclient: get nodes: %w", err)
	}
	return resp.Node, nil
}
