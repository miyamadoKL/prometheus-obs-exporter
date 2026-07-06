package collector

import (
	"net/http"
	"testing"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient/obsclienttest"
)

const (
	testUsername = obsclienttest.Username
	testPassword = obsclienttest.Password

	// authTokenHeader mirrors obsclient's unexported constant of the same
	// name (internal/obsclient/auth.go), via obsclienttest.
	authTokenHeader = obsclienttest.AuthTokenHeader
)

// serveFixture writes the contents of internal/collector/testdata/name as a
// JSON response body. Thin wrapper around obsclienttest, which is also used
// by internal/obsclient's own black-box tests (see
// internal/obsclient/obsclienttest).
func serveFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	obsclienttest.ServeFixture(t, w, name)
}

// newTestClient builds an obsclient.Client pointed at an
// httptest.NewTLSServer server, with TLS verification disabled (the test
// server uses a self-signed certificate).
func newTestClient(t *testing.T, serverURL string, username, password string) *obsclient.Client {
	t.Helper()
	return obsclienttest.NewTestClient(t, serverURL, username, password)
}
