package client

import (
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

func NewTrackerClient(baseURL string, timeout time.Duration) (*TrackerClient, error) {
	endpoint, err := newJSONClient(baseURL, timeout)
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

func (c *TrackerClient) Register(node chord.NodeInfo) error {
	return c.endpoint.do(http.MethodPost, "/tracker/nodes", node.Core(), nil)
}

func (c *TrackerClient) Deregister(nodeID string) error {
	return c.endpoint.do(http.MethodDelete, "/tracker/nodes/"+url.PathEscape(nodeID), nil, nil)
}

func (c *TrackerClient) Heartbeat(nodeID string, heartbeat chord.TrackerHeartbeat) error {
	return c.endpoint.do(http.MethodPost, "/tracker/nodes/"+url.PathEscape(nodeID)+"/heartbeat", heartbeat, nil)
}
