package client

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"chorddht/internal/chord"
)

type TrackerClient struct {
	endpoint jsonClient
}

func NewTrackerClient(baseURL string, timeout time.Duration, skipTLSVerify bool) (*TrackerClient, error) {
	// Tracker client never signs requests (tracker is not a Chord peer).
	endpoint, err := newJSONClient(baseURL, timeout, skipTLSVerify, nil)
	if err != nil {
		return nil, err
	}
	return &TrackerClient{endpoint: endpoint}, nil
}

func (c *TrackerClient) Seeds(count int, exclude []string) ([]chord.NodeInfo, error) {
	values := url.Values{}
	if count > 0 {
		values.Set("count", strconv.Itoa(count))
	}
	if len(exclude) > 0 {
		values.Set("exclude", strings.Join(exclude, ","))
	}
	values.Set("include_cert", "true")
	var resp struct {
		Seeds      []chord.NodeInfo `json:"seeds"`
		TotalKnown int              `json:"total_known"`
		Note       string           `json:"note"`
	}
	if err := c.endpoint.do(http.MethodGet, appendQuery("/tracker/nodes/seeds", values), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Seeds, nil
}

func (c *TrackerClient) Register(node chord.NodeInfo) (string, error) {
	body := struct {
		NodeID      string             `json:"node_id"`
		URI         string             `json:"uri"`
		Certificate json.RawMessage    `json:"certificate,omitempty"`
		Region      string             `json:"region,omitempty"`
		Vnodes      []chord.VNodeEntry `json:"vnodes,omitempty"`
	}{
		NodeID:      node.NodeID,
		URI:         node.URI,
		Certificate: node.Certificate,
		Region:      node.Region,
		Vnodes:      node.Vnodes,
	}
	var resp struct {
		Region *string `json:"region"`
	}
	if err := c.endpoint.do(http.MethodPost, "/tracker/nodes", body, &resp); err != nil {
		return "", err
	}
	if resp.Region != nil {
		return *resp.Region, nil
	}
	return "", nil
}

func (c *TrackerClient) Deregister(nodeID string) error {
	return c.endpoint.do(http.MethodDelete, "/tracker/nodes/"+url.PathEscape(nodeID), nil, nil)
}

func (c *TrackerClient) DetectRegion() (string, error) {
	var resp struct {
		Region  *string `json:"region"`
		Country *string `json:"country"`
	}
	if err := c.endpoint.do(http.MethodGet, "/tracker/geo", nil, &resp); err != nil {
		return "", err
	}
	if resp.Region != nil && *resp.Region != "" {
		return *resp.Region, nil
	}
	if resp.Country != nil && *resp.Country != "" {
		return *resp.Country, nil
	}
	return "", nil
}

func (c *TrackerClient) Heartbeat(nodeID string, heartbeat chord.TrackerHeartbeat) error {
	return c.endpoint.do(http.MethodPost, "/tracker/nodes/"+url.PathEscape(nodeID)+"/heartbeat", heartbeat, nil)
}

// FetchCRL retrieves the raw CRL JSON from the tracker's GET /tracker/crl endpoint.
func (c *TrackerClient) FetchCRL() ([]byte, error) {
	var raw json.RawMessage
	if err := c.endpoint.do(http.MethodGet, "/tracker/crl", nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
