// Package obsclient implements an HTTP client for the Dell ECS 3.8.x /
// ObjectScale 4.x management, metering and Flux (time-series) REST APIs.
//
// This file defines the contract types: typed representations of the JSON
// (and XML, for the DT-stats/ping endpoints) response bodies returned by
// those APIs. Collector code (internal/collector) should depend only on
// these types and the client methods in dashboard.go / metering.go /
// dtstats.go / flux.go, never on raw JSON.
package obsclient

import (
	"errors"
	"strconv"
	"strings"
)

// ErrMissing is returned by FlexNumber.Float64 / FlexNumber.Int64 when the
// underlying JSON field was absent or null (decoded as the empty string).
// Callers (collector code) treat this the same as any other parse error:
// the "err == nil" guard already in place means a missing field simply
// causes that metric series to not be emitted, rather than emitting a
// misleading fake zero.
var ErrMissing = errors.New("obsclient: value missing")

// FlexNumber decodes a JSON numeric value that the ECS / ObjectScale
// dashboard and metering APIs may represent either as a native JSON number
// (e.g. 123, 1.5) or as a quoted JSON string (e.g. "123", "1.5") -
// both forms have been observed across API versions for the same field.
// It stores the literal text and exposes Float64/Int64 accessors, similar
// to encoding/json.Number but tolerant of the quoted form as well.
type FlexNumber string

// UnmarshalJSON implements json.Unmarshaler. It accepts both a bare JSON
// number token and a JSON string containing a number.
func (n *FlexNumber) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" {
		*n = ""
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	*n = FlexNumber(s)
	return nil
}

// Float64 parses the number as a float64. An empty value (field absent or
// null) returns (0, ErrMissing) rather than a fake zero.
func (n FlexNumber) Float64() (float64, error) {
	if n == "" {
		return 0, ErrMissing
	}
	return strconv.ParseFloat(string(n), 64)
}

// Int64 parses the number as an int64. An empty value (field absent or
// null) returns (0, ErrMissing) rather than a fake zero.
func (n FlexNumber) Int64() (int64, error) {
	if n == "" {
		return 0, ErrMissing
	}
	return strconv.ParseInt(string(n), 10, 64)
}

func (n FlexNumber) String() string { return string(n) }

// ---------------------------------------------------------------------
// Auth: GET /login, GET /user/whoami, GET /logout
// ---------------------------------------------------------------------

// WhoAmI is the response body of GET /user/whoami, used to validate that a
// cached auth token is still usable.
type WhoAmI struct {
	CommonName string   `json:"common_name"`
	Roles      []string `json:"roles"`
}

// ---------------------------------------------------------------------
// Dashboard API: GET /dashboard/zones/localzone
// ---------------------------------------------------------------------

// CountSample is a single historical sample of a "Num*" style series in the
// dashboard API, e.g. one element of alertsNumUnackCritical.
type CountSample struct {
	Count FlexNumber `json:"Count"`
}

// SpaceSample is a single historical sample of a disk space series in the
// dashboard API, e.g. one element of diskSpaceTotalCurrent. The unit is GB
// per docs/design.md; conversion to bytes is a collector-layer concern.
type SpaceSample struct {
	Space FlexNumber `json:"Space"`
}

// FirstCount returns the Count of the first (current) sample in a Count
// series, or (0, false) if the series is empty.
func FirstCount(samples []CountSample) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	v, err := samples[0].Count.Float64()
	if err != nil {
		return 0, false
	}
	return v, true
}

// FirstSpace returns the Space of the first (current) sample in a Space
// series, or (0, false) if the series is empty.
func FirstSpace(samples []SpaceSample) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	v, err := samples[0].Space.Float64()
	if err != nil {
		return 0, false
	}
	return v, true
}

// LocalZone is the response body of GET /dashboard/zones/localzone.
// ECS 3.6.0.0+ removed the transaction* / diskRead(Write)BandwidthTotal /
// nodeCpuUtilization fields documented for older versions; only fields
// still present per docs/design.md are modeled here.
type LocalZone struct {
	Name string `json:"name"`

	NumBadDisks  FlexNumber `json:"numBadDisks"`
	NumBadNodes  FlexNumber `json:"numBadNodes"`
	NumGoodNodes FlexNumber `json:"numGoodNodes"`
	NumGoodDisks FlexNumber `json:"numGoodDisks"`

	AlertsNumUnackCritical []CountSample `json:"alertsNumUnackCritical"`
	AlertsNumUnackError    []CountSample `json:"alertsNumUnackError"`
	AlertsNumUnackInfo     []CountSample `json:"alertsNumUnackInfo"`
	AlertsNumUnackWarning  []CountSample `json:"alertsNumUnackWarning"`

	DiskSpaceFreeCurrent      []SpaceSample `json:"diskSpaceFreeCurrent"`
	DiskSpaceTotalCurrent     []SpaceSample `json:"diskSpaceTotalCurrent"`
	DiskSpaceAllocatedCurrent []SpaceSample `json:"diskSpaceAllocatedCurrent"`
}

// ---------------------------------------------------------------------
// Dashboard API: GET /dashboard/zones/localzone/replicationgroups
// ---------------------------------------------------------------------

// ReplicationGroup is a single entry of the replicationgroup array returned
// by GET /dashboard/zones/localzone/replicationgroups. The old exporter
// mislabeled this data as "node"; docs/design.md renames the collector
// label to "rg" (replication group name).
type ReplicationGroup struct {
	Name string `json:"name"`

	ReplicationIngressTraffic                FlexNumber `json:"replicationIngressTraffic"`
	ReplicationEgressTraffic                 FlexNumber `json:"replicationEgressTraffic"`
	ChunksRepoPendingReplicationTotalSize    FlexNumber `json:"chunksRepoPendingReplicationTotalSize"`
	ChunksJournalPendingReplicationTotalSize FlexNumber `json:"chunksJournalPendingReplicationTotalSize"`
	ChunksPendingXorTotalSize                FlexNumber `json:"chunksPendingXorTotalSize"`
	// ReplicationRpoTimestamp is an epoch timestamp; docs/design.md notes
	// the unit needs conversion to epoch seconds at the collector layer
	// (the dashboard API has been observed to return epoch milliseconds).
	ReplicationRpoTimestamp FlexNumber `json:"replicationRpoTimestamp"`
}

// ReplicationGroupsResponse is the top-level response body of
// GET /dashboard/zones/localzone/replicationgroups.
type ReplicationGroupsResponse struct {
	Replicationgroup []ReplicationGroup `json:"replicationgroup"`
}

// ---------------------------------------------------------------------
// Dashboard API: GET /vdc/nodes
// ---------------------------------------------------------------------

// Node is a single entry of the node array returned by GET /vdc/nodes.
type Node struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	DataIP  string `json:"data_ip"`
	MgmtIP  string `json:"mgmt_ip"`
	Version string `json:"version"`
}

// NodesResponse is the top-level response body of GET /vdc/nodes.
type NodesResponse struct {
	Node []Node `json:"node"`
}

// ---------------------------------------------------------------------
// Metering API: /object/namespaces, /object/namespaces/namespace/{ns}/quota,
// /object/billing/namespace/{ns}/info
// ---------------------------------------------------------------------

// NamespaceRef is a single entry of the namespace array returned by
// GET /object/namespaces.
type NamespaceRef struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// NamespacesResponse is the top-level response body of
// GET /object/namespaces.
type NamespacesResponse struct {
	Namespace []NamespaceRef `json:"namespace"`
}

// NamespaceQuota is the response body of
// GET /object/namespaces/namespace/{namespace}/quota. Sizes are in GB per
// docs/design.md; conversion to bytes is a collector-layer concern.
type NamespaceQuota struct {
	Namespace        string     `json:"namespace"`
	BlockSize        FlexNumber `json:"blockSize"`
	NotificationSize FlexNumber `json:"notificationSize"`
}

// NamespaceBillingInfo is the response body of
// GET /object/billing/namespace/{namespace}/info. total_size is in the unit
// requested via the sizeunit query parameter (obsclient always requests
// KB); conversion to bytes is a collector-layer concern.
type NamespaceBillingInfo struct {
	Namespace    string     `json:"namespace"`
	TotalObjects FlexNumber `json:"total_objects"`
	TotalSize    FlexNumber `json:"total_size"`
}

// ---------------------------------------------------------------------
// DT statistics: http://<node>:9101/stats/dt/DTInitStat (XML)
// ---------------------------------------------------------------------

// DTInitStat is the parsed body of http://<node>:9101/stats/dt/DTInitStat.
// The root element name is not documented; only the <entry> child and its
// descendants are relied upon (matches the old ecsclient.go behavior).
type DTInitStat struct {
	TotalDTNum   float64 `xml:"entry>total_dt_num"`
	UnreadyDTNum float64 `xml:"entry>unready_dt_num"`
	UnknownDTNum float64 `xml:"entry>unknown_dt_num"`
}

// ---------------------------------------------------------------------
// Ping / active connections: https://<node>:<objPort>/?ping (XML)
// ---------------------------------------------------------------------

// PingResponse is the parsed body of https://<node>:<objPort>/?ping.
type PingResponse struct {
	Xmlns  string   `xml:"xmlns,attr"`
	Name   []string `xml:"PingItem>Name"`
	Value  float64  `xml:"PingItem>Value"`
	Status []string `xml:"PingItem>Status"`
	Text   []string `xml:"PingItem>Text"`
}

// ---------------------------------------------------------------------
// Flux API: POST /flux/api/external/v2/query
// ---------------------------------------------------------------------

// fluxQueryRequest is the JSON request body sent to the Flux API.
type fluxQueryRequest struct {
	Query string `json:"query"`
}

// FluxSeries is a single element of the "Series" array in a Flux API JSON
// response. Values contains one row per data point; every cell is a string
// (per docs/api-research/flux-api-reference.md, all Flux JSON values are
// string representations regardless of Datatypes).
type FluxSeries struct {
	Datatypes []string   `json:"Datatypes"`
	Columns   []string   `json:"Columns"`
	Values    [][]string `json:"Values"`
}

// FluxQueryResponse is the top-level JSON response body of
// POST /flux/api/external/v2/query.
type FluxQueryResponse struct {
	Series []FluxSeries `json:"Series"`
}
