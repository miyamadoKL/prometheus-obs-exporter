package obsclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const (
	testUsername = "monitor"
	testPassword = "s3cr3t"
)

// TestLoginSetsToken verifies GET /login is called with Basic auth and the
// X-SDS-AUTH-TOKEN response header is captured.
func TestLoginSetsToken(t *testing.T) {
	var loginCalls int32

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != testUsername || pass != testPassword {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		atomic.AddInt32(&loginCalls, 1)
		w.Header().Set(authTokenHeader, "token-1")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)

	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if c.authToken != "token-1" {
		t.Fatalf("authToken = %q, want %q", c.authToken, "token-1")
	}
	if got := atomic.LoadInt32(&loginCalls); got != 1 {
		t.Fatalf("login calls = %d, want 1", got)
	}
}

// TestLoginFailureNoToken verifies a non-200 or missing-token /login
// response is surfaced as an error.
func TestLoginFailureNoToken(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	if err := c.Login(context.Background()); err == nil {
		t.Fatal("Login: expected error, got nil")
	}
}

// TestAuthenticatedRequestRetriesOnceOn401 verifies that a request which
// receives a 401 (simulating an expired/invalid cached token) triggers
// exactly one re-login and one retry, after which a valid token succeeds.
func TestAuthenticatedRequestRetriesOnceOn401(t *testing.T) {
	var loginCalls, dataCalls int32

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			n := atomic.AddInt32(&loginCalls, 1)
			w.Header().Set(authTokenHeader, tokenForLogin(n))
			w.WriteHeader(http.StatusOK)
		case "/dashboard/zones/localzone":
			atomic.AddInt32(&dataCalls, 1)
			token := r.Header.Get(authTokenHeader)
			if token != "token-2" {
				// The first token issued is treated as already-expired,
				// forcing exactly one re-login + retry.
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			serveFixture(t, w, "localzone.json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)

	lz, err := c.GetLocalZone(context.Background())
	if err != nil {
		t.Fatalf("GetLocalZone: %v", err)
	}
	if lz.Name != "vdc1" {
		t.Fatalf("Name = %q, want vdc1", lz.Name)
	}
	if got := atomic.LoadInt32(&loginCalls); got != 2 {
		t.Fatalf("login calls = %d, want 2 (initial + one retry)", got)
	}
	if got := atomic.LoadInt32(&dataCalls); got != 2 {
		t.Fatalf("data calls = %d, want 2 (initial 401 + retry)", got)
	}
}

// TestAuthenticatedRequestGivesUpAfterOneRetry verifies the client does NOT
// recurse indefinitely (unlike the original exporter) when every request
// keeps returning 401: it must attempt at most one re-login and surface an
// error rather than looping.
func TestAuthenticatedRequestGivesUpAfterOneRetry(t *testing.T) {
	var loginCalls, dataCalls int32

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			atomic.AddInt32(&loginCalls, 1)
			w.Header().Set(authTokenHeader, "always-invalid")
			w.WriteHeader(http.StatusOK)
		case "/dashboard/zones/localzone":
			atomic.AddInt32(&dataCalls, 1)
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)

	if _, err := c.GetLocalZone(context.Background()); err == nil {
		t.Fatal("GetLocalZone: expected error, got nil")
	}
	if got := atomic.LoadInt32(&loginCalls); got != 2 {
		t.Fatalf("login calls = %d, want 2 (initial + exactly one retry)", got)
	}
	if got := atomic.LoadInt32(&dataCalls); got != 2 {
		t.Fatalf("data calls = %d, want 2 (initial + exactly one retry)", got)
	}
}

// TestWhoAmI verifies GET /user/whoami decodes correctly and can be used to
// validate a cached token.
func TestWhoAmI(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/user/whoami":
			if r.Header.Get(authTokenHeader) != "token-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"common_name":"monitor","roles":["SYSTEM_MONITOR"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)

	who, err := c.WhoAmI(context.Background())
	if err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}
	if who.CommonName != "monitor" {
		t.Fatalf("CommonName = %q, want monitor", who.CommonName)
	}
	if len(who.Roles) != 1 || who.Roles[0] != "SYSTEM_MONITOR" {
		t.Fatalf("Roles = %v, want [SYSTEM_MONITOR]", who.Roles)
	}
}

// TestLogout verifies /logout is called with the current token and that
// the client tolerates being logged out twice (matching ECS/ObjectScale's
// documented behavior of returning 401 once already logged out) without
// making a redundant request the second time (no token cached).
func TestLogout(t *testing.T) {
	var logoutCalls int32

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set(authTokenHeader, "token-1")
			w.WriteHeader(http.StatusOK)
		case "/logout":
			atomic.AddInt32(&logoutCalls, 1)
			if r.Header.Get(authTokenHeader) != "token-1" {
				t.Errorf("logout token = %q, want token-1", r.Header.Get(authTokenHeader))
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, testUsername, testPassword)
	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := c.Logout(context.Background()); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if c.authToken != "" {
		t.Fatalf("authToken = %q, want empty after Logout", c.authToken)
	}
	// Second Logout with no cached token must be a no-op (no HTTP call).
	if err := c.Logout(context.Background()); err != nil {
		t.Fatalf("second Logout: %v", err)
	}
	if got := atomic.LoadInt32(&logoutCalls); got != 1 {
		t.Fatalf("logout calls = %d, want 1", got)
	}
}

func tokenForLogin(n int32) string {
	if n <= 1 {
		return "token-1"
	}
	return "token-2"
}
