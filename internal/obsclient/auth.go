package obsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// authTokenHeader は ECS / ObjectScale が /login から発行したてのトークンを
// 返すのにも、以降のリクエストでそれを受け付けるのにも使うヘッダー。
const authTokenHeader = "X-SDS-AUTH-TOKEN"

// Login は HTTP Basic 認証で GET /login に対して認証を行い、得られた
// X-SDS-AUTH-TOKEN を以降のリクエスト用に保存する。並行呼び出しに対して
// 安全であり、同時に呼んでも重複ログインは起きない。
func (c *Client) Login(ctx context.Context) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	return c.login(ctx)
}

// login は実際の /login 呼び出しを行う。呼び出し側は authMu を保持している
// こと。
func (c *Client) login(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.mgmtURL("/login"), nil)
	if err != nil {
		return fmt.Errorf("obsclient: building login request: %w", err)
	}
	req.SetBasicAuth(c.cfg.Username, c.cfg.Password)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("obsclient: login request to %s: %w", c.Target, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("obsclient: login to %s failed with status %s", c.Target, resp.Status)
	}

	token := resp.Header.Get(authTokenHeader)
	if token == "" {
		return fmt.Errorf("obsclient: login response from %s did not include %s header", c.Target, authTokenHeader)
	}

	c.authToken = token
	return nil
}

// Logout は、トークンを保持していれば GET /logout でそれを無効化する。
// 既にログアウト済みの状態で呼んでも安全（他の Logout 呼び出しと同時でも）で、
// ECS/ObjectScale がユーザーごとの有効トークン数を100件に制限していることから、
// SIGTERM/SIGINT 時にキャッシュ済みの全クライアントに対して呼ばれることを
// 想定している。
func (c *Client) Logout(ctx context.Context) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()

	if c.authToken == "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.mgmtURL("/logout"), nil)
	if err != nil {
		return fmt.Errorf("obsclient: building logout request: %w", err)
	}
	req.Header.Set(authTokenHeader, c.authToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("obsclient: logout request to %s: %w", c.Target, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	c.authToken = ""

	switch resp.StatusCode {
	case http.StatusOK, http.StatusUnauthorized:
		// ここでの 401 は単に既にログアウト済みだったことを意味する。
		return nil
	default:
		return fmt.Errorf("obsclient: logout from %s failed with status %s", c.Target, resp.Status)
	}
}

// WhoAmI は GET /user/whoami を呼び出す。これは現在のトークンがまだ使える
// ことを検証すると同時に、認証済みユーザーの identity と roles を返す。
func (c *Client) WhoAmI(ctx context.Context) (*WhoAmI, error) {
	var who WhoAmI
	if err := c.getAuthenticatedJSON(ctx, "/user/whoami", &who); err != nil {
		return nil, fmt.Errorf("obsclient: whoami: %w", err)
	}
	return &who, nil
}

// ensureLoggedIn は現在のトークンを返し、キャッシュがなければ先にログインする。
// リクエスト全体ではなくここだけで authMu を保持することで、複数の実行中の
// リクエストが1つのトークンを共有しつつ、HTTP 呼び出しごとに直列化することなく
// 重複ログインも防げる。
func (c *Client) ensureLoggedIn(ctx context.Context) (string, error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.authToken == "" {
		if err := c.login(ctx); err != nil {
			return "", err
		}
	}
	return c.authToken, nil
}

// invalidateToken はキャッシュされたトークンをクリアするが、それが old と
// まだ一致している場合のみ行う - こうすることで、あるリクエストからの
// 遅れた無効化が、別のリクエストが再ログインで既に取得した新しいトークンを
// 潰してしまうのを防ぐ。
func (c *Client) invalidateToken(old string) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.authToken == old {
		c.authToken = ""
	}
}

func (c *Client) requestWithToken(ctx context.Context, method, url string, body []byte, token string) (*rawResponse, error) {
	headers := http.Header{}
	headers.Set(authTokenHeader, token)
	headers.Set("Accept", "application/json")
	if body != nil {
		headers.Set("Content-Type", "application/json")
		return c.post(ctx, url, body, headers)
	}
	return c.get(ctx, url, headers)
}

// callAuthenticatedJSON は url に対して認証付きリクエストを実行し、（非nilなら）
// out へ JSON レスポンス body をデコードする。最初の試行が 401 を受け取った
// 場合、キャッシュされたトークンを無効化し、1回だけ再ログインを試みて
// リクエストを1回だけリトライする - 旧 exporter にあった 401 時の
// 無制限な再帰リトライは意図的に再現していない。
func (c *Client) callAuthenticatedJSON(ctx context.Context, method, url string, body []byte, out any) error {
	token, err := c.ensureLoggedIn(ctx)
	if err != nil {
		return err
	}

	resp, err := c.requestWithToken(ctx, method, url, body, token)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		c.invalidateToken(token)

		token, err = c.ensureLoggedIn(ctx)
		if err != nil {
			return fmt.Errorf("obsclient: re-login after 401 from %s: %w", url, err)
		}

		resp, err = c.requestWithToken(ctx, method, url, body, token)
		if err != nil {
			return err
		}
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("obsclient: %s %s: unexpected status %d: %s", method, url, resp.StatusCode, truncateBody(resp.Body))
	}

	if out != nil {
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return fmt.Errorf("obsclient: decoding response from %s: %w", url, err)
		}
	}
	return nil
}

// getAuthenticatedJSON は指定された management-API パス（相対パス、例:
// "/dashboard/zones/localzone"）に対して認証付き GET を実行する。
func (c *Client) getAuthenticatedJSON(ctx context.Context, path string, out any) error {
	return c.callAuthenticatedJSON(ctx, http.MethodGet, c.mgmtURL(path), nil, out)
}

// postAuthenticatedJSON は指定された management-API パスに対して JSON body
// 付きの認証済み POST を実行する。
func (c *Client) postAuthenticatedJSON(ctx context.Context, path string, body []byte, out any) error {
	return c.callAuthenticatedJSON(ctx, http.MethodPost, c.mgmtURL(path), body, out)
}

func truncateBody(b []byte) string {
	const max = 512
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}
