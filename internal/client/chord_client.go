package client

import (
	"net/http"
	"time"

	"chorddht/internal/auth"
	"chorddht/internal/chord"
)

// TimeoutConfig holds per-operation tiered timeouts for same vs. cross-region peers.
type TimeoutConfig struct {
	PingSame         time.Duration
	PingCross        time.Duration
	FindSuccessorSame time.Duration
	FindSuccessorCross time.Duration
	FixFingersSame   time.Duration
	FixFingersCross  time.Duration
	Default          time.Duration
}

type ChordClient struct {
	timeout        time.Duration
	skipTLSVerify  bool
	signer         *auth.RequestSigner // nil when auth is disabled
	selfRegion     string
	timeouts       TimeoutConfig
	nodeRegionFunc func(nodeID string) string // optional: look up a peer's region by node ID
}

// NewChordClient creates a new peer client. Pass nil signer to disable auth.
func NewChordClient(timeout time.Duration, skipTLSVerify bool, signer *auth.RequestSigner) *ChordClient {
	return &ChordClient{
		timeout:       timeout,
		skipTLSVerify: skipTLSVerify,
		signer:        signer,
		timeouts: TimeoutConfig{
			PingSame:           chord.DefaultTimeoutPingSameRegion,
			PingCross:          chord.DefaultTimeoutPingCrossRegion,
			FindSuccessorSame:  chord.DefaultTimeoutFindSuccessorSame,
			FindSuccessorCross: chord.DefaultTimeoutFindSuccessorCross,
			FixFingersSame:     chord.DefaultTimeoutFixFingersSame,
			FixFingersCross:    chord.DefaultTimeoutFixFingersCross,
			Default:            timeout,
		},
	}
}

// SetSelfRegion configures the region label for this node (used to determine same/cross region).
func (c *ChordClient) SetSelfRegion(region string) { c.selfRegion = region }

// SetTimeoutConfig overrides the tiered timeout configuration.
func (c *ChordClient) SetTimeoutConfig(tc TimeoutConfig) { c.timeouts = tc }

// SetNodeRegionFunc sets a callback that returns the region for a given peer node ID.
func (c *ChordClient) SetNodeRegionFunc(f func(nodeID string) string) { c.nodeRegionFunc = f }

// peerRegion returns the region for the given peer URI, or "" if unknown.
func (c *ChordClient) peerRegion(uri string) string {
	if c.nodeRegionFunc == nil {
		return ""
	}
	nodeID, err := chord.HashURI(uri)
	if err != nil {
		return ""
	}
	return c.nodeRegionFunc(nodeID)
}

// isSameRegion reports whether the peer at uri is in the same region as this node.
func (c *ChordClient) isSameRegion(uri string) bool {
	if c.selfRegion == "" {
		return false
	}
	pr := c.peerRegion(uri)
	return pr != "" && pr == c.selfRegion
}

// timeoutFor returns the appropriate timeout for the given operation and peer URI.
func (c *ChordClient) timeoutFor(op, uri string) time.Duration {
	same := c.isSameRegion(uri)
	switch op {
	case "ping":
		if same {
			return c.timeouts.PingSame
		}
		return c.timeouts.PingCross
	case "find_successor":
		if same {
			return c.timeouts.FindSuccessorSame
		}
		return c.timeouts.FindSuccessorCross
	case "fix_fingers":
		if same {
			return c.timeouts.FixFingersSame
		}
		return c.timeouts.FixFingersCross
	default:
		if c.timeouts.Default > 0 {
			return c.timeouts.Default
		}
		return c.timeout
	}
}

func (c *ChordClient) endpoint(uri string) (jsonClient, error) {
	return c.endpointFor(uri, "default")
}

func (c *ChordClient) endpointFor(uri, op string) (jsonClient, error) {
	if err := requireHTTPSURI(uri); err != nil {
		return jsonClient{}, err
	}
	return newJSONClient(uri, c.timeoutFor(op, uri), c.skipTLSVerify, c.signer)
}

func (c *ChordClient) Ping(uri string) error {
	endpoint, err := c.endpointFor(uri, "ping")
	if err != nil {
		return err
	}
	return endpoint.do(http.MethodGet, "/chord/ping", nil, &chord.PingResponse{})
}

func (c *ChordClient) PingWithLatency(uri string) (int64, error) {
	endpoint, err := c.endpointFor(uri, "ping")
	if err != nil {
		return 0, err
	}
	start := timeNow()
	if err := endpoint.do(http.MethodGet, "/chord/ping", nil, &chord.PingResponse{}); err != nil {
		return 0, err
	}
	return timeNow().Sub(start).Milliseconds(), nil
}

func (c *ChordClient) RTT(uri string) (chord.RTTResponse, error) {
	endpoint, err := c.endpointFor(uri, "ping")
	if err != nil {
		return chord.RTTResponse{}, err
	}
	var resp chord.RTTResponse
	err = endpoint.do(http.MethodGet, "/chord/rtt", nil, &resp)
	return resp, err
}

func (c *ChordClient) FindSuccessor(uri string, req chord.FindSuccessorRequest) (chord.FindSuccessorResponse, error) {
	endpoint, err := c.endpointFor(uri, "find_successor")
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
