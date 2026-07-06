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

// Config holds the per-target connection settings needed to build a Client.
// It mirrors the ecs.* settings in internal/config.
type Config struct {
	Username string
	Password string

	// MgmtPort is used for every management/dashboard/metering/flux
	// request. Unlike the original exporter, it is never hardcoded.
	MgmtPort int
	// ObjPort is used only for the node ping ("active connections") check.
	ObjPort int

	TLSInsecureSkipVerify bool
	// TLSCAFile, if set, is a PEM file used to verify the target's TLS
	// certificate in addition to the system trust store.
	TLSCAFile string

	// Timeout applies to each individual HTTP request.
	Timeout time.Duration
}

// Client is an HTTP client for a single ECS / ObjectScale management
// endpoint ("target"). It is safe for concurrent use: request execution
// itself is unsynchronized (allowing concurrent scrapes, e.g. metering
// namespace fan-out), while the cached auth token is protected by a mutex
// to prevent duplicate logins (see auth.go).
type Client struct {
	Target string

	cfg  Config
	http *http.Client

	authMu    sync.Mutex
	authToken string
}

// New builds a Client for the given target host (no scheme/port, e.g.
// "ecs1.example.com" or "10.0.0.1"). It performs no network I/O; call
// Login to authenticate.
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

// mgmtURL builds an https URL against the configured management port for
// the given absolute path (which must start with "/").
func (c *Client) mgmtURL(path string) string {
	return fmt.Sprintf("https://%s:%d%s", c.Target, c.cfg.MgmtPort, path)
}

// rawResponse is a fully-drained HTTP response: reading the body eagerly
// keeps callers from having to worry about closing it, at the cost of
// buffering the (small, JSON/XML) response bodies this client deals with.
type rawResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// doRequest performs a single HTTP request and returns its fully-read
// response. It does not know about authentication; see auth.go for the
// authenticated GET/POST helpers used by dashboard.go, metering.go and
// flux.go.
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

// get performs an unauthenticated GET request.
func (c *Client) get(ctx context.Context, url string, headers http.Header) (*rawResponse, error) {
	return c.doRequest(ctx, http.MethodGet, url, nil, headers)
}

// post performs an unauthenticated POST request.
func (c *Client) post(ctx context.Context, url string, body []byte, headers http.Header) (*rawResponse, error) {
	return c.doRequest(ctx, http.MethodPost, url, body, headers)
}
