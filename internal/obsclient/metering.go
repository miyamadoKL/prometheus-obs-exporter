package obsclient

import (
	"context"
	"fmt"
	"net/url"
)

// ListNamespaces fetches GET /object/namespaces, returning every object
// namespace defined on the cluster. Metering collection fans out per
// namespace from this list, so it is only ever called when the metering
// collector is explicitly requested (it is comparatively expensive).
func (c *Client) ListNamespaces(ctx context.Context) ([]NamespaceRef, error) {
	var resp NamespacesResponse
	if err := c.getAuthenticatedJSON(ctx, "/object/namespaces", &resp); err != nil {
		return nil, fmt.Errorf("obsclient: list namespaces: %w", err)
	}
	return resp.Namespace, nil
}

// GetNamespaceQuota fetches
// GET /object/namespaces/namespace/{namespace}/quota.
//
// Like every other request made by this client, the request always goes to
// the configured management port (Config.MgmtPort); the original exporter
// hardcoded :4443 for metering calls, which broke on clusters using a
// non-default management port.
func (c *Client) GetNamespaceQuota(ctx context.Context, namespace string) (*NamespaceQuota, error) {
	path := "/object/namespaces/namespace/" + url.PathEscape(namespace) + "/quota"
	var q NamespaceQuota
	if err := c.getAuthenticatedJSON(ctx, path, &q); err != nil {
		return nil, fmt.Errorf("obsclient: get namespace quota for %q: %w", namespace, err)
	}
	return &q, nil
}

// GetNamespaceBilling fetches
// GET /object/billing/namespace/{namespace}/info?sizeunit=KB, which reports
// current object count and usage for a namespace. Aggregation delay is up
// to 15 minutes (up to 2h15m for S3 fan-out) per docs/design.md.
func (c *Client) GetNamespaceBilling(ctx context.Context, namespace string) (*NamespaceBillingInfo, error) {
	path := "/object/billing/namespace/" + url.PathEscape(namespace) + "/info?sizeunit=KB"
	var info NamespaceBillingInfo
	if err := c.getAuthenticatedJSON(ctx, path, &info); err != nil {
		return nil, fmt.Errorf("obsclient: get namespace billing info for %q: %w", namespace, err)
	}
	return &info, nil
}
