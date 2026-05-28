package client

import (
	"net/http"
	"time"

	"chorddht/internal/chord"
)

type ChordClient struct {
	timeout       time.Duration
	skipTLSVerify bool
}

func NewChordClient(timeout time.Duration, skipTLSVerify bool) *ChordClient {
	return &ChordClient{timeout: timeout, skipTLSVerify: skipTLSVerify}
}

func (c *ChordClient) endpoint(uri string) (jsonClient, error) {
	if err := requireHTTPSURI(uri); err != nil {
		return jsonClient{}, err
	}
	return newJSONClient(uri, c.timeout, c.skipTLSVerify)
}

func (c *ChordClient) Ping(uri string) error {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return err
	}
	return endpoint.do(http.MethodGet, "/chord/ping", nil, &chord.PingResponse{})
}

func (c *ChordClient) FindSuccessor(uri string, req chord.FindSuccessorRequest) (chord.FindSuccessorResponse, error) {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return chord.FindSuccessorResponse{}, err
	}
	var resp chord.FindSuccessorResponse
	return resp, endpoint.do(http.MethodPost, "/chord/find_successor", req, &resp)
}

func (c *ChordClient) Join(uri string, req chord.JoinRequest) (chord.JoinResponse, error) {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return chord.JoinResponse{}, err
	}
	var resp chord.JoinResponse
	return resp, endpoint.do(http.MethodPost, "/chord/join", req, &resp)
}

func (c *ChordClient) Notify(uri string, req chord.NotifyRequest) (chord.NotifyResponse, error) {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return chord.NotifyResponse{}, err
	}
	var resp chord.NotifyResponse
	return resp, endpoint.do(http.MethodPost, "/chord/notify", req, &resp)
}

func (c *ChordClient) Predecessor(uri string) (chord.PredecessorResponse, error) {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return chord.PredecessorResponse{}, err
	}
	var resp chord.PredecessorResponse
	return resp, endpoint.do(http.MethodGet, "/chord/predecessor", nil, &resp)
}

func (c *ChordClient) SuccessorList(uri string) (chord.SuccessorListResponse, error) {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return chord.SuccessorListResponse{}, err
	}
	var resp chord.SuccessorListResponse
	return resp, endpoint.do(http.MethodGet, "/chord/successor_list", nil, &resp)
}

func (c *ChordClient) Leave(uri string, req chord.LeaveRequest) error {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return err
	}
	return endpoint.do(http.MethodPost, "/chord/leave", req, &chord.LeaveResponse{})
}
