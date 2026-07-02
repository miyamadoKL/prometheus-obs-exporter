package obsclient

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

const dtInitStatXML = `<blob>
  <entry>
    <total_dt_num>128</total_dt_num>
    <unready_dt_num>1</unready_dt_num>
    <unknown_dt_num>0</unknown_dt_num>
  </entry>
</blob>`

const pingXML = `<ns2:blobtag xmlns:ns2="http://example.com/ping" xmlns="http://example.com/ping">
  <PingItem>
    <Name>ActiveConnections</Name>
    <Value>17</Value>
    <Status>OK</Status>
    <Text>ok</Text>
  </PingItem>
</ns2:blobtag>`

// TestDTInitStatXMLDecoding verifies the DTInitStat contract type decodes
// the DT statistics XML shape used by http://<node>:9101/stats/dt/DTInitStat.
func TestDTInitStatXMLDecoding(t *testing.T) {
	var stat DTInitStat
	if err := xml.Unmarshal([]byte(dtInitStatXML), &stat); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if stat.TotalDTNum != 128 {
		t.Errorf("TotalDTNum = %v, want 128", stat.TotalDTNum)
	}
	if stat.UnreadyDTNum != 1 {
		t.Errorf("UnreadyDTNum = %v, want 1", stat.UnreadyDTNum)
	}
	if stat.UnknownDTNum != 0 {
		t.Errorf("UnknownDTNum = %v, want 0", stat.UnknownDTNum)
	}
}

// TestPingResponseXMLDecoding verifies the PingResponse contract type
// decodes the ping XML shape used by https://<node>:<objPort>/?ping.
func TestPingResponseXMLDecoding(t *testing.T) {
	var ping PingResponse
	if err := xml.Unmarshal([]byte(pingXML), &ping); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if ping.Value != 17 {
		t.Errorf("Value = %v, want 17", ping.Value)
	}
	if len(ping.Name) != 1 || ping.Name[0] != "ActiveConnections" {
		t.Errorf("Name = %v, want [ActiveConnections]", ping.Name)
	}
}

// TestGetNodeDTStats exercises GetNodeDTStats end-to-end against a local
// HTTPS server standing in for the ping endpoint. The DT-stats endpoint
// (http://<node>:9101/...) uses a hardcoded port per the ECS/ObjectScale
// API and cannot be redirected to a test server's random port, so it is
// exercised only via the XML-decoding unit tests above; this test instead
// verifies error propagation when the DT-stats endpoint is unreachable
// while confirming ping accounting/error semantics via GetAllNodeDTStats.
func TestGetAllNodeDTStatsIsolatesPerNodeErrors(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parsing test server port: %v", err)
	}

	c, err := New(u.Hostname(), Config{
		Username:              testUsername,
		Password:              testPassword,
		ObjPort:               port,
		TLSInsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("obsclient.New: %v", err)
	}

	results := c.GetAllNodeDTStats(context.Background(), []string{u.Hostname(), u.Hostname()})
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, r := range results {
		// Both the (unreachable, port 9101) DT-stats call and the (404)
		// ping call are expected to fail here; the important behavior
		// under test is that each node's failure is captured in NodeDTStats
		// rather than aborting the whole batch or panicking.
		if r.Err == nil {
			t.Errorf("node %s: Err = nil, want a DT-stats/ping error", r.Node)
		}
	}
}
