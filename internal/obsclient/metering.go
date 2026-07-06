package obsclient

import (
	"context"
	"fmt"
	"net/url"
)

// ListNamespaces は GET /object/namespaces を取得し、クラスタ上に定義された
// 全 object namespace を返す。metering の収集はこの一覧から namespace ごとに
// ファンアウトするので、metering コレクターが明示的に要求されたときにしか
// 呼ばれない（比較的コストが高いため）。
func (c *Client) ListNamespaces(ctx context.Context) ([]NamespaceRef, error) {
	var resp NamespacesResponse
	if err := c.getAuthenticatedJSON(ctx, "/object/namespaces", &resp); err != nil {
		return nil, fmt.Errorf("obsclient: list namespaces: %w", err)
	}
	return resp.Namespace, nil
}

// GetNamespaceQuota は
// GET /object/namespaces/namespace/{namespace}/quota を取得する。
//
// このクライアントの他の全リクエストと同様、リクエストは常に設定済みの
// management ポート（Config.MgmtPort）へ向かう。元の exporter は metering の
// 呼び出しで :4443 をハードコードしており、デフォルト以外の management
// ポートを使うクラスタで壊れていた。
func (c *Client) GetNamespaceQuota(ctx context.Context, namespace string) (*NamespaceQuota, error) {
	path := "/object/namespaces/namespace/" + url.PathEscape(namespace) + "/quota"
	var q NamespaceQuota
	if err := c.getAuthenticatedJSON(ctx, path, &q); err != nil {
		return nil, fmt.Errorf("obsclient: get namespace quota for %q: %w", namespace, err)
	}
	return &q, nil
}

// GetNamespaceBilling は
// GET /object/billing/namespace/{namespace}/info?sizeunit=KB を取得する。
// これは namespace の現在のオブジェクト数と使用量を報告する。集計の遅延は
// docs/design.md によれば最大15分（S3 のファンアウトを考慮すると最大2時間15分）。
func (c *Client) GetNamespaceBilling(ctx context.Context, namespace string) (*NamespaceBillingInfo, error) {
	path := "/object/billing/namespace/" + url.PathEscape(namespace) + "/info?sizeunit=KB"
	var info NamespaceBillingInfo
	if err := c.getAuthenticatedJSON(ctx, path, &info); err != nil {
		return nil, fmt.Errorf("obsclient: get namespace billing info for %q: %w", namespace, err)
	}
	return &info, nil
}
