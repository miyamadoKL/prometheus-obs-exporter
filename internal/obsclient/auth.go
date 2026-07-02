package obsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// authTokenHeader is the header ECS / ObjectScale uses both to return a
// freshly-issued token from /login and to accept it on subsequent requests.
const authTokenHeader = "X-SDS-AUTH-TOKEN"

// Login authenticates against GET /login using HTTP Basic auth and stores
// the resulting X-SDS-AUTH-TOKEN for use by subsequent requests. It is safe
// to call concurrently; concurrent callers will not trigger duplicate
// logins.
func (c *Client) Login(ctx context.Context) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	return c.login(ctx)
}

// login performs the actual /login call. Callers must hold authMu.
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

// Logout invalidates the current token via GET /logout, if one is held. It
// is safe to call when already logged out (including concurrently with
// other Logout calls) and is intended to be called for every cached client
// on SIGTERM/SIGINT, since ECS/ObjectScale caps active tokens per user
// at 100.
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
		// 401 here just means we were already logged out.
		return nil
	default:
		return fmt.Errorf("obsclient: logout from %s failed with status %s", c.Target, resp.Status)
	}
}

// WhoAmI calls GET /user/whoami, which both validates that the current
// token is still usable and returns the authenticated user's identity and
// roles.
func (c *Client) WhoAmI(ctx context.Context) (*WhoAmI, error) {
	var who WhoAmI
	if err := c.getAuthenticatedJSON(ctx, "/user/whoami", &who); err != nil {
		return nil, fmt.Errorf("obsclient: whoami: %w", err)
	}
	return &who, nil
}

// ensureLoggedIn returns the current token, logging in first if none is
// cached. Holding authMu here (rather than for the whole request) is what
// lets multiple in-flight requests share one token without serializing on
// every HTTP call, while still preventing duplicate logins.
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

// invalidateToken clears the cached token, but only if it still matches
// old - this avoids a late invalidation from one request clobbering a
// newer token another request already obtained via re-login.
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

// callAuthenticatedJSON performs an authenticated request against url,
// decoding a JSON response body into out (if non-nil). If the first attempt
// receives a 401, the cached token is invalidated, a single re-login is
// attempted, and the request is retried exactly once - the old exporter's
// unbounded recursive retry-on-401 is intentionally not reproduced.
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

// getAuthenticatedJSON performs an authenticated GET against the given
// management-API path (relative, e.g. "/dashboard/zones/localzone").
func (c *Client) getAuthenticatedJSON(ctx context.Context, path string, out any) error {
	return c.callAuthenticatedJSON(ctx, http.MethodGet, c.mgmtURL(path), nil, out)
}

// postAuthenticatedJSON performs an authenticated POST with a JSON body
// against the given management-API path.
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
