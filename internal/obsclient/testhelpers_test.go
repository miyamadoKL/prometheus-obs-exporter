package obsclient

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// serveFixture writes the contents of internal/obsclient/testdata/name as a
// JSON response body.
func serveFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %q: %v", name, err)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(data); err != nil {
		t.Fatalf("writing fixture %q: %v", name, err)
	}
}

// newTestClient builds a Client pointed at an httptest.NewTLSServer server,
// with TLS verification disabled (the test server uses a self-signed
// certificate) so real request/response plumbing (including auth) is
// exercised end-to-end.
func newTestClient(t *testing.T, serverURL string, username, password string) *Client {
	t.Helper()

	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parsing test server URL %q: %v", serverURL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parsing test server port from %q: %v", serverURL, err)
	}

	c, err := New(u.Hostname(), Config{
		Username:              username,
		Password:              password,
		MgmtPort:              port,
		TLSInsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("obsclient.New: %v", err)
	}
	return c
}
