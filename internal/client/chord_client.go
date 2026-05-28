package client

import (
	"net/http"
	"time"

	"chorddht/internal/auth"
	"chorddht/internal/chord"
)

type ChordClient struct {
	timeout       time.Duration
	skipTLSVerify bool
	signer        *auth.RequestSigner // nil when auth is disabled
}

// NewChordClient creates a new peer client. Pass nil signer to disable auth.
func NewChordClient(timeout time.Duration, skipTLSVerify bool, signer *auth.RequestSigner) *ChordClient {
	return &ChordClient{timeout: timeout, skipTLSVerify: skipTLSVerify, signer: signer}
}

func (c *ChordClient) endpoint(uri string) (jsonClient, error) {
	if err := requireHTTPSURI(uri); err != nil {
		return jsonClient{}, err
	}
	return newJSONClient(uri, c.timeout, c.skipTLSVerify, c.signer)
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
	err = endpoint.doSigned(http.MethodPost, "/chord/find_successor", req, &resp, false)
	if isCertRequired(err) {
		err = endpoint.doSigned(http.MethodPost, "/chord/find_successor", req, &resp, true)
	}
	return resp, err
}

func (c *ChordClient) Join(uri string, req chord.JoinRequest) (chord.JoinResponse, error) {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return chord.JoinResponse{}, err
	}
	var resp chord.JoinResponse
	// Join always sends the cert in the body; also include X-Chord-Certificate on first try.
	err = endpoint.doSigned(http.MethodPost, "/chord/join", req, &resp, true)
	return resp, err
}

func (c *ChordClient) Notify(uri string, req chord.NotifyRequest) (chord.NotifyResponse, error) {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return chord.NotifyResponse{}, err
	}
	var resp chord.NotifyResponse
	// Notify always sends cert in body; include header on first try.
	err = endpoint.doSigned(http.MethodPost, "/chord/notify", req, &resp, true)
	return resp, err
}

func (c *ChordClient) Predecessor(uri string) (chord.PredecessorResponse, error) {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return chord.PredecessorResponse{}, err
	}
	var resp chord.PredecessorResponse
	err = endpoint.doSigned(http.MethodGet, "/chord/predecessor", nil, &resp, false)
	if isCertRequired(err) {
		err = endpoint.doSigned(http.MethodGet, "/chord/predecessor", nil, &resp, true)
	}
	return resp, err
}

func (c *ChordClient) SuccessorList(uri string) (chord.SuccessorListResponse, error) {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return chord.SuccessorListResponse{}, err
	}
	var resp chord.SuccessorListResponse
	err = endpoint.doSigned(http.MethodGet, "/chord/successor_list", nil, &resp, false)
	if isCertRequired(err) {
		err = endpoint.doSigned(http.MethodGet, "/chord/successor_list", nil, &resp, true)
	}
	return resp, err
}

func (c *ChordClient) Leave(uri string, req chord.LeaveRequest) error {
	endpoint, err := c.endpoint(uri)
	if err != nil {
		return err
	}
	err = endpoint.doSigned(http.MethodPost, "/chord/leave", req, &chord.LeaveResponse{}, false)
	if isCertRequired(err) {
		err = endpoint.doSigned(http.MethodPost, "/chord/leave", req, &chord.LeaveResponse{}, true)
	}
	return err
}
