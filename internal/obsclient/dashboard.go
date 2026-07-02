package obsclient

import (
	"context"
	"fmt"
)

// GetLocalZone fetches GET /dashboard/zones/localzone, which reports the
// current node/disk health counts, unacknowledged alert counts and
// capacity for the local VDC.
func (c *Client) GetLocalZone(ctx context.Context) (*LocalZone, error) {
	var lz LocalZone
	if err := c.getAuthenticatedJSON(ctx, "/dashboard/zones/localzone", &lz); err != nil {
		return nil, fmt.Errorf("obsclient: get local zone: %w", err)
	}
	return &lz, nil
}

// GetReplicationGroups fetches
// GET /dashboard/zones/localzone/replicationgroups, which reports current
// replication throughput and backlog per replication group.
func (c *Client) GetReplicationGroups(ctx context.Context) ([]ReplicationGroup, error) {
	var resp ReplicationGroupsResponse
	if err := c.getAuthenticatedJSON(ctx, "/dashboard/zones/localzone/replicationgroups", &resp); err != nil {
		return nil, fmt.Errorf("obsclient: get replication groups: %w", err)
	}
	return resp.Replicationgroup, nil
}

// GetNodes fetches GET /vdc/nodes, which lists every node in the VDC along
// with its management/data IPs and the cluster software version.
func (c *Client) GetNodes(ctx context.Context) ([]Node, error) {
	var resp NodesResponse
	if err := c.getAuthenticatedJSON(ctx, "/vdc/nodes", &resp); err != nil {
		return nil, fmt.Errorf("obsclient: get nodes: %w", err)
	}
	return resp.Node, nil
}
