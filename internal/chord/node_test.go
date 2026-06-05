package chord

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"
)

// recordingTracker captures Register calls for assertion in tests.
type recordingTracker struct {
	registered []NodeInfo
}

func (r *recordingTracker) Seeds(_ int, _ []string) ([]NodeInfo, error) { return nil, nil }
func (r *recordingTracker) Register(node NodeInfo) (string, error) {
	r.registered = append(r.registered, node)
	return "", nil
}
func (r *recordingTracker) Deregister(_ string) error                    { return nil }
func (r *recordingTracker) Heartbeat(_ string, _ TrackerHeartbeat) error { return nil }
func (r *recordingTracker) FetchCRL() ([]byte, error)                    { return nil, nil }

// TestVNodeTrackerRegistration verifies that:
//  1. Anchor's JoinNetwork registers with the tracker.
//  2. Vnode's JoinNetwork does NOT call tracker.Register (guard in registerTracker).
//  3. Calling RegisterTracker on the anchor after SetVNodeEntries sends the full vnode list.
func TestVNodeTrackerRegistration(t *testing.T) {
	tracker := &recordingTracker{}

	// Create anchor node.
	anchorOpts := DefaultOptions()
	anchor, err := NewNode("https://anchor.example.com", anchorOpts, nil, tracker)
	if err != nil {
		t.Fatal(err)
	}
	anchor.JoinNetwork(nil) // single-node ring; calls registerTracker once
	if len(tracker.registered) != 1 {
		t.Fatalf("expected 1 tracker.Register call after anchor JoinNetwork, got %d", len(tracker.registered))
	}
	if len(tracker.registered[0].Vnodes) != 0 {
		t.Fatalf("expected empty vnodes on initial anchor registration, got %d", len(tracker.registered[0].Vnodes))
	}

	// Derive anchor ID and sign a vnode proof.
	anchorID := anchor.Self().NodeID
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	proof := SignVNodeProof(anchorID, 1, privKey, DefaultMaintenanceInterval)

	vnodeOpts := anchorOpts
	vnodeOpts.VNodeIndex = 1
	vnodeOpts.AnchorID = anchorID
	vnodeOpts.VNodeProofPtr = proof
	vnode, err := NewNode("https://anchor.example.com", vnodeOpts, nil, tracker)
	if err != nil {
		t.Fatal(err)
	}
	vnode.JoinNetwork(nil) // must NOT call tracker.Register
	if len(tracker.registered) != 1 {
		t.Fatalf("vnode JoinNetwork must not call tracker.Register; got %d total calls", len(tracker.registered))
	}

	// Re-register anchor with vnode entries.
	entry := VNodeEntry{VNodeID: vnode.Self().NodeID, Index: 1, Proof: proof}
	anchor.SetVNodeEntries([]VNodeEntry{entry})
	anchor.RegisterTracker()
	if len(tracker.registered) != 2 {
		t.Fatalf("expected 2 tracker.Register calls after anchor RegisterTracker, got %d", len(tracker.registered))
	}
	got := tracker.registered[1].Vnodes
	if len(got) != 1 || got[0].VNodeID != entry.VNodeID {
		t.Fatalf("expected vnode entry in re-registration, got %+v", got)
	}
}

func TestSingleNodeFindSuccessorReturnsSelf(t *testing.T) {
	node, err := NewNode("https://node1.example.com", DefaultOptions(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	node.ActivateSingleNode()
	resp, err := node.HandleFindSuccessor(FindSuccessorRequest{ID: "0000000000000000000000000000000000000000", MaxHops: DefaultMaxHops})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Found || resp.Successor == nil || resp.Successor.NodeID != node.Self().NodeID {
		t.Fatalf("expected self successor, got %+v", resp)
	}
}

func TestNotifyAcceptsCloserPredecessor(t *testing.T) {
	node, err := NewNode("https://node3.example.com", DefaultOptions(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	node.ActivateSingleNode()
	candidate, err := NewNodeInfoFromURI("https://node2.example.com")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := node.HandleNotify(NotifyRequest{Node: candidate})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Accepted || resp.Predecessor == nil || resp.Predecessor.NodeID != candidate.NodeID {
		t.Fatalf("expected candidate predecessor to be accepted, got %+v", resp)
	}
}

// errClient is a PeerClient that fails every call, used to simulate an unreachable peer.
type errClient struct{}

func (errClient) Ping(_ string) error                     { return errors.New("unreachable") }
func (errClient) PingLiveness(_ string) error             { return errors.New("unreachable") }
func (errClient) PingWithLatency(_ string) (int64, error) { return 0, errors.New("unreachable") }
func (errClient) FindSuccessor(_ NodeInfo, _ FindSuccessorRequest) (FindSuccessorResponse, error) {
	return FindSuccessorResponse{}, errors.New("unreachable")
}
func (errClient) Join(_ string, _ JoinRequest) (JoinResponse, error) {
	return JoinResponse{}, errors.New("unreachable")
}
func (errClient) Notify(_ NodeInfo, _ NotifyRequest) (NotifyResponse, error) {
	return NotifyResponse{}, errors.New("unreachable")
}
func (errClient) Rectify(_ NodeInfo, _ RectifyRequest) (RectifyResponse, error) {
	return RectifyResponse{}, errors.New("unreachable")
}
func (errClient) State(_ NodeInfo) (StateResponse, error) {
	return StateResponse{}, errors.New("unreachable")
}
func (errClient) Predecessor(_ NodeInfo) (PredecessorResponse, error) {
	return PredecessorResponse{}, errors.New("unreachable")
}
func (errClient) SuccessorList(_ NodeInfo) (SuccessorListResponse, error) {
	return SuccessorListResponse{}, errors.New("unreachable")
}
func (errClient) Leave(_ NodeInfo, _ LeaveRequest) error { return errors.New("unreachable") }
func (errClient) RTT(_ string) (RTTResponse, error)      { return RTTResponse{}, errors.New("unreachable") }

// TestStabilizeIsolatesWhenAllPeersFail verifies that a node transitions to
// StatusIsolated (not stays ACTIVE) when its successor and all backups are unreachable.
func TestStabilizeIsolatesWhenAllPeersFail(t *testing.T) {
	node, err := NewNode("https://node1.example.com", DefaultOptions(), errClient{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	node.ActivateSingleNode()

	// Simulate having joined a real ring: point successor at a remote peer.
	peer, err := NewNodeInfoFromURI("https://node2.example.com")
	if err != nil {
		t.Fatal(err)
	}
	node.mu.Lock()
	node.successor = peer.Core()
	node.successorList = node.mergeSuccessorListLocked(peer.Core(), []NodeInfo{peer.Core()})
	node.mu.Unlock()

	node.Stabilize()

	if got := node.Self().Status; got != StatusIsolated {
		t.Fatalf("expected StatusIsolated after all peers fail, got %s", got)
	}
}

// TestStabilizeSingleNodeRingStaysActive verifies that a fresh single-node ring
// remains ACTIVE after Stabilize (it is not falsely marked isolated).
func TestStabilizeSingleNodeRingStaysActive(t *testing.T) {
	node, err := NewNode("https://node1.example.com", DefaultOptions(), errClient{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	node.ActivateSingleNode()

	node.Stabilize()

	if got := node.Self().Status; got != StatusActive {
		t.Fatalf("expected StatusActive for single-node ring, got %s", got)
	}
}

type retryJoinClient struct {
	mu                    sync.Mutex
	joinCalls             int
	failuresBeforeSuccess int
	successor             NodeInfo
}

func (c *retryJoinClient) Ping(_ string) error         { return nil }
func (c *retryJoinClient) PingLiveness(_ string) error { return nil }
func (c *retryJoinClient) PingWithLatency(_ string) (int64, error) {
	return 1, nil
}
func (c *retryJoinClient) FindSuccessor(_ NodeInfo, _ FindSuccessorRequest) (FindSuccessorResponse, error) {
	c.mu.Lock()
	successor := c.successor
	c.mu.Unlock()
	return FindSuccessorResponse{Found: true, Successor: &successor}, nil
}
func (c *retryJoinClient) Join(_ string, _ JoinRequest) (JoinResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.joinCalls++
	if c.joinCalls <= c.failuresBeforeSuccess {
		return JoinResponse{}, errors.New("join unavailable")
	}
	return JoinResponse{Successor: c.successor, SuccessorList: []NodeInfo{c.successor}}, nil
}
func (c *retryJoinClient) Notify(_ NodeInfo, _ NotifyRequest) (NotifyResponse, error) {
	return NotifyResponse{Accepted: true}, nil
}
func (c *retryJoinClient) Rectify(_ NodeInfo, _ RectifyRequest) (RectifyResponse, error) {
	return RectifyResponse{Accepted: true}, nil
}
func (c *retryJoinClient) State(_ NodeInfo) (StateResponse, error) {
	c.mu.Lock()
	successor := c.successor
	c.mu.Unlock()
	return StateResponse{SuccessorList: []NodeInfo{successor}, SuccessorListValid: true}, nil
}
func (c *retryJoinClient) Predecessor(_ NodeInfo) (PredecessorResponse, error) {
	return PredecessorResponse{}, nil
}
func (c *retryJoinClient) SuccessorList(_ NodeInfo) (SuccessorListResponse, error) {
	c.mu.Lock()
	successor := c.successor
	c.mu.Unlock()
	return SuccessorListResponse{SuccessorList: []NodeInfo{successor}}, nil
}
func (c *retryJoinClient) Leave(_ NodeInfo, _ LeaveRequest) error { return nil }
func (c *retryJoinClient) RTT(_ string) (RTTResponse, error) {
	return RTTResponse{Samples: map[string]int64{}}, nil
}
func (c *retryJoinClient) JoinCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.joinCalls
}

type sequenceTracker struct {
	mu            sync.Mutex
	seedResponses [][]NodeInfo
	seedCalls     int
	registered    []NodeInfo
}

func (t *sequenceTracker) Seeds(_ int, _ []string) ([]NodeInfo, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	index := t.seedCalls
	t.seedCalls++
	if index >= len(t.seedResponses) {
		index = len(t.seedResponses) - 1
	}
	if index < 0 {
		return nil, nil
	}
	return cloneNodes(t.seedResponses[index]), nil
}
func (t *sequenceTracker) Register(node NodeInfo) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.registered = append(t.registered, node)
	return "", nil
}
func (t *sequenceTracker) Deregister(_ string) error                    { return nil }
func (t *sequenceTracker) Heartbeat(_ string, _ TrackerHeartbeat) error { return nil }
func (t *sequenceTracker) FetchCRL() ([]byte, error)                    { return nil, nil }
func (t *sequenceTracker) SeedCalls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seedCalls
}

func TestActiveSingletonRetriesStoredBootstrapSeeds(t *testing.T) {
	seed, err := NewNodeInfoFromURI("https://node2.example.com")
	if err != nil {
		t.Fatal(err)
	}
	client := &retryJoinClient{failuresBeforeSuccess: 1, successor: seed}
	node, err := NewNode("https://node1.example.com", DefaultOptions(), client, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := node.JoinNetwork([]NodeInfo{seed}); err != nil {
		t.Fatal(err)
	}
	if got := client.JoinCalls(); got != 1 {
		t.Fatalf("expected one initial join attempt, got %d", got)
	}
	if state := node.State(); state.Successor.NodeID != node.Self().NodeID {
		t.Fatalf("expected initial failed join to activate singleton, got successor %s", state.Successor.NodeID)
	}

	node.MaintenanceCycle()

	if got := client.JoinCalls(); got < 2 {
		t.Fatalf("expected maintenance to retry join, got %d attempts", got)
	}
	if state := node.State(); state.Successor.NodeID != seed.NodeID {
		t.Fatalf("expected retry to join seed successor %s, got %s", seed.NodeID, state.Successor.NodeID)
	}
}

func TestActiveSingletonRetriesFreshTrackerSeeds(t *testing.T) {
	seed, err := NewNodeInfoFromURI("https://node2.example.com")
	if err != nil {
		t.Fatal(err)
	}
	tracker := &sequenceTracker{seedResponses: [][]NodeInfo{nil, {seed}}}
	client := &retryJoinClient{successor: seed}
	node, err := NewNode("https://node1.example.com", DefaultOptions(), client, tracker)
	if err != nil {
		t.Fatal(err)
	}

	if err := node.JoinNetwork(nil); err != nil {
		t.Fatal(err)
	}
	if got := client.JoinCalls(); got != 0 {
		t.Fatalf("expected no peer join when tracker initially has no seeds, got %d", got)
	}
	if state := node.State(); state.Successor.NodeID != node.Self().NodeID {
		t.Fatalf("expected initial tracker miss to activate singleton, got successor %s", state.Successor.NodeID)
	}

	node.MaintenanceCycle()

	if got := tracker.SeedCalls(); got < 2 {
		t.Fatalf("expected maintenance to fetch fresh tracker seeds, got %d seed calls", got)
	}
	if got := client.JoinCalls(); got != 1 {
		t.Fatalf("expected one join after tracker seed appears, got %d", got)
	}
	if state := node.State(); state.Successor.NodeID != seed.NodeID {
		t.Fatalf("expected tracker retry to join seed successor %s, got %s", seed.NodeID, state.Successor.NodeID)
	}
}

func TestActiveSingletonWithoutSeedsDoesNotRetryJoin(t *testing.T) {
	seed, err := NewNodeInfoFromURI("https://node2.example.com")
	if err != nil {
		t.Fatal(err)
	}
	client := &retryJoinClient{successor: seed}
	node, err := NewNode("https://node1.example.com", DefaultOptions(), client, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := node.JoinNetwork(nil); err != nil {
		t.Fatal(err)
	}
	node.MaintenanceCycle()

	if got := client.JoinCalls(); got != 0 {
		t.Fatalf("expected no retry without tracker or saved seeds, got %d join attempts", got)
	}
	if got := node.Self().Status; got != StatusActive {
		t.Fatalf("expected singleton to remain active, got %s", got)
	}
}

func TestValidateNodeInfoRejectsMismatchedID(t *testing.T) {
	info, err := NewNodeInfoFromURI("https://node1.example.com")
	if err != nil {
		t.Fatal(err)
	}
	info.NodeID = "0000000000000000000000000000000000000000"
	if err := ValidateNodeInfo(info); err == nil {
		t.Fatal("expected mismatched id to be rejected")
	}
}

func TestRectifyAcceptsCandidateWhenCurrentPredecessorDead(t *testing.T) {
	client := &scriptedPeerClient{live: map[string]bool{"https://pred.example.com": false}}
	node, err := NewNode("https://self.example.com", DefaultOptions(), client, nil)
	if err != nil {
		t.Fatal(err)
	}
	node.ActivateSingleNode()
	self := fixedNode("80", "https://self.example.com")
	pred := fixedNode("70", "https://pred.example.com")
	candidate := fixedNode("10", "https://candidate.example.com")
	node.mu.Lock()
	node.self = self
	node.successor = self.Core()
	node.successorList = []NodeInfo{self.Core()}
	node.predecessor = &pred
	node.mu.Unlock()

	resp, err := node.HandleRectify(RectifyRequest{Node: candidate})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Accepted || resp.Predecessor == nil || resp.Predecessor.NodeID != candidate.NodeID {
		t.Fatalf("expected dead predecessor to be replaced, got %+v", resp)
	}
}

func TestStabilizeIgnoresDeadCandidatePredecessor(t *testing.T) {
	self := fixedNode("10", "https://self.example.com")
	successor := fixedNode("50", "https://successor.example.com")
	deadCandidate := fixedNode("30", "https://dead.example.com")
	client := &scriptedPeerClient{
		live: map[string]bool{
			deadCandidate.URI: false,
			successor.URI:     true,
		},
		states: map[string]StateResponse{
			successor.NodeID: {
				Predecessor:        &deadCandidate,
				SuccessorList:      []NodeInfo{},
				SuccessorListValid: true,
			},
		},
	}
	node, err := NewNode("https://self.example.com", DefaultOptions(), client, nil)
	if err != nil {
		t.Fatal(err)
	}
	node.ActivateSingleNode()
	node.mu.Lock()
	node.self = self
	node.successor = successor
	node.successorList = []NodeInfo{successor}
	node.mu.Unlock()

	node.Stabilize()

	state := node.State()
	if state.Successor.NodeID != successor.NodeID {
		t.Fatalf("expected successor to remain %s, got %s", successor.NodeID, state.Successor.NodeID)
	}
	if !state.SuccessorListValid {
		t.Fatal("expected successor list to remain valid")
	}
}

func TestJoinInitializesSuccessorListFromSuccessorState(t *testing.T) {
	seed := fixedNode("20", "https://seed.example.com")
	successor := fixedNode("50", "https://successor.example.com")
	next := fixedNode("70", "https://next.example.com")
	client := &scriptedPeerClient{
		live: map[string]bool{
			successor.URI: true,
			next.URI:      true,
		},
		joinResp: JoinResponse{Successor: successor, SuccessorList: []NodeInfo{}},
		states: map[string]StateResponse{
			successor.NodeID: {SuccessorList: []NodeInfo{next}, SuccessorListValid: true},
			next.NodeID:      {SuccessorList: []NodeInfo{}, SuccessorListValid: true},
		},
	}
	node, err := NewNode("https://self.example.com", DefaultOptions(), client, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := node.JoinNetwork([]NodeInfo{seed}); err != nil {
		t.Fatal(err)
	}

	state := node.State()
	if len(state.SuccessorList) < 2 {
		t.Fatalf("expected successor list to include successor and successor's successor, got %+v", state.SuccessorList)
	}
	if state.SuccessorList[0].NodeID != successor.NodeID || state.SuccessorList[1].NodeID != next.NodeID {
		t.Fatalf("unexpected successor list order: %+v", state.SuccessorList)
	}
}

func TestStateIncludesV5InvariantMetadata(t *testing.T) {
	node, err := NewNode("https://node1.example.com", DefaultOptions(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	node.ActivateSingleNode()
	state := node.State()
	if !state.SuccessorListValid {
		t.Fatal("expected single-node successor list to be valid")
	}
	if state.LastInvariantCheck == nil {
		t.Fatal("expected last invariant check timestamp")
	}
	if state.SnapshotTimestamp.IsZero() {
		t.Fatal("expected snapshot timestamp")
	}
}

type scriptedPeerClient struct {
	live     map[string]bool
	states   map[string]StateResponse
	joinResp JoinResponse
}

func (c *scriptedPeerClient) Ping(uri string) error { return c.PingLiveness(uri) }
func (c *scriptedPeerClient) PingLiveness(uri string) error {
	if c.live != nil && !c.live[uri] {
		return errors.New("unreachable")
	}
	return nil
}
func (c *scriptedPeerClient) PingWithLatency(uri string) (int64, error) {
	if err := c.PingLiveness(uri); err != nil {
		return 0, err
	}
	return 1, nil
}
func (c *scriptedPeerClient) FindSuccessor(_ NodeInfo, _ FindSuccessorRequest) (FindSuccessorResponse, error) {
	return FindSuccessorResponse{Found: true, Successor: &c.joinResp.Successor}, nil
}
func (c *scriptedPeerClient) Join(_ string, _ JoinRequest) (JoinResponse, error) {
	return c.joinResp, nil
}
func (c *scriptedPeerClient) Notify(_ NodeInfo, _ NotifyRequest) (NotifyResponse, error) {
	return NotifyResponse{Accepted: true}, nil
}
func (c *scriptedPeerClient) Rectify(_ NodeInfo, _ RectifyRequest) (RectifyResponse, error) {
	return RectifyResponse{Accepted: true}, nil
}
func (c *scriptedPeerClient) State(target NodeInfo) (StateResponse, error) {
	if state, ok := c.states[target.NodeID]; ok {
		return state, nil
	}
	return StateResponse{SuccessorList: []NodeInfo{}, SuccessorListValid: true}, nil
}
func (c *scriptedPeerClient) Predecessor(target NodeInfo) (PredecessorResponse, error) {
	state, err := c.State(target)
	if err != nil {
		return PredecessorResponse{}, err
	}
	return PredecessorResponse{Predecessor: state.Predecessor}, nil
}
func (c *scriptedPeerClient) SuccessorList(target NodeInfo) (SuccessorListResponse, error) {
	state, err := c.State(target)
	if err != nil {
		return SuccessorListResponse{}, err
	}
	return SuccessorListResponse{SuccessorList: state.SuccessorList}, nil
}
func (c *scriptedPeerClient) Leave(_ NodeInfo, _ LeaveRequest) error { return nil }
func (c *scriptedPeerClient) RTT(_ string) (RTTResponse, error) {
	return RTTResponse{Samples: map[string]int64{}}, nil
}

func fixedNode(prefix, uri string) NodeInfo {
	return NodeInfo{NodeID: prefix + strings.Repeat("0", 40-len(prefix)), URI: uri, AnchorID: "anchor"}
}
