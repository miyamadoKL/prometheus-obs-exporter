package obsclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	defaultDialTimeout   = 10 * time.Second
	defaultClientTimeout = 30 * time.Second

	defaultMgmtPort = 4443
	defaultObjPort  = 9021
)

// Config は Client を構築するのに必要な、ターゲットごとの接続設定を保持する。
// internal/config の ecs.* 設定に対応する。
type Config struct {
	Username string
	Password string

	// MgmtPort は management/dashboard/metering/flux の全リクエストで
	// 使われる。旧 exporter と異なり、ハードコードされることはない。
	MgmtPort int
	// ObjPort は node ping（"active connections"）チェックにのみ使われる。
	ObjPort int

	TLSInsecureSkipVerify bool
	// TLSCAFile が設定されている場合、システムのトラストストアに加えて
	// ターゲットの TLS 証明書を検証するのに使う PEM ファイル。
	TLSCAFile string

	// Timeout は個々の HTTP リクエストごとに適用される。
	Timeout time.Duration
}

// Client は単一の ECS / ObjectScale 管理エンドポイント（"target"）向けの
// HTTP クライアント。並行利用に対して安全: リクエストの実行自体は
// 同期を取らない（metering の namespace ファンアウトなど並行スクレイプを
// 許すため）が、キャッシュされた認証トークンは重複ログインを防ぐために
// mutex で保護される（auth.go 参照）。
type Client struct {
	Target string

	cfg  Config
	http *http.Client

	authMu    sync.Mutex
	authToken string
}

// New は指定された target ホスト（スキーム/ポートなし、例:
// "ecs1.example.com" や "10.0.0.1"）向けの Client を構築する。ネットワーク
// I/O は一切行わない。認証するには Login を呼ぶこと。
func New(target string, cfg Config) (*Client, error) {
	if target == "" {
		return nil, fmt.Errorf("obsclient: target must not be empty")
	}
	if cfg.MgmtPort == 0 {
		cfg.MgmtPort = defaultMgmtPort
	}
	if cfg.ObjPort == 0 {
		cfg.ObjPort = defaultObjPort
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultClientTimeout
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: cfg.TLSInsecureSkipVerify} //nolint:gosec // opt-in via config, not a default

	if cfg.TLSCAFile != "" {
		pool, err := loadCAFile(cfg.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("obsclient: loading CA file: %w", err)
		}
		tlsConfig.RootCAs = pool
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   defaultDialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConfig,
	}

	return &Client{
		Target: target,
		cfg:    cfg,
		http: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
	}, nil
}

func loadCAFile(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(data); !ok {
		return nil, fmt.Errorf("no certificates found in %s", path)
	}
	return pool, nil
}

// mgmtURL は、設定済みの management ポートに対する絶対パス（"/" で
// 始まる必要がある）から https URL を構築する。
func (c *Client) mgmtURL(path string) string {
	return fmt.Sprintf("https://%s:%d%s", c.Target, c.cfg.MgmtPort, path)
}

// rawResponse は body を読み切った HTTP レスポンス: body を先読みすることで
// 呼び出し側が close を気にする必要がなくなる。代わりに、このクライアントが
// 扱う（小さな JSON/XML の）レスポンス body をバッファリングするコストを払う。
type rawResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// doRequest は単一の HTTP リクエストを実行し、読み切ったレスポンスを返す。
// 認証については関知しない。dashboard.go, metering.go, flux.go が使う
// 認証付き GET/POST ヘルパーは auth.go を参照。
func (c *Client) doRequest(ctx context.Context, method, url string, body []byte, headers http.Header) (*rawResponse, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("obsclient: building request: %w", err)
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("obsclient: request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("obsclient: reading response from %s: %w", url, err)
	}

	return &rawResponse{StatusCode: resp.StatusCode, Header: resp.Header, Body: b}, nil
}

// get は未認証の GET リクエストを実行する。
func (c *Client) get(ctx context.Context, url string, headers http.Header) (*rawResponse, error) {
	return c.doRequest(ctx, http.MethodGet, url, nil, headers)
}

// post は未認証の POST リクエストを実行する。
func (c *Client) post(ctx context.Context, url string, body []byte, headers http.Header) (*rawResponse, error) {
	return c.doRequest(ctx, http.MethodPost, url, body, headers)
}
