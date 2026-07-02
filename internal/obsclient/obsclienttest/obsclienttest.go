// Package obsclienttest provides shared httptest fixture-server plumbing
// used by both internal/obsclient's own black-box tests and
// internal/collector's tests (which need a real, authenticated
// obsclient.Client backed by a fixture server). It exists to avoid
// duplicating this plumbing between the two packages.
//
// internal/obsclient/auth_test.go deliberately does not use this package:
// it is a white-box (package obsclient) test that asserts against the
// unexported authToken field, and an in-package test file cannot import a
// package that itself imports obsclient - that would be an import cycle.
// It keeps a small private copy of ServeFixture/NewTestClient instead (see
// internal/obsclient/testhelpers_test.go).
package obsclienttest

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
)

// AuthTokenHeader mirrors obsclient's unexported authTokenHeader constant
// (internal/obsclient/auth.go); duplicated here since it cannot be
// imported.
const AuthTokenHeader = "X-SDS-AUTH-TOKEN"

// Username and Password are the credentials fixture servers can expect and
// that NewTestClient configures its Client with.
const (
	Username = "monitor"
	Password = "s3cr3t"
)

// ServeFixture writes the contents of testdata/name (resolved relative to
// the running test binary's working directory, i.e. the package under
// test's directory) as a JSON response body.
func ServeFixture(t *testing.T, w http.ResponseWriter, name string) {
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

// NewTestClient builds an obsclient.Client pointed at an
// httptest.NewTLSServer server, with TLS verification disabled (the test
// server uses a self-signed certificate) so real request/response plumbing
// (including auth) is exercised end-to-end.
func NewTestClient(t *testing.T, serverURL, username, password string) *obsclient.Client {
	t.Helper()

	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parsing test server URL %q: %v", serverURL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parsing test server port from %q: %v", serverURL, err)
	}

	c, err := obsclient.New(u.Hostname(), obsclient.Config{
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
