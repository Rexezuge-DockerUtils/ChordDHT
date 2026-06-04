package chord

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

// recordingTracker captures Register calls for assertion in tests.
type recordingTracker struct {
	registered []NodeInfo
}

func (r *recordingTracker) Seeds(_ int, _ []string) ([]NodeInfo, error)       { return nil, nil }
func (r *recordingTracker) Register(node NodeInfo) (string, error)            { r.registered = append(r.registered, node); return "", nil }
func (r *recordingTracker) Deregister(_ string) error                         { return nil }
func (r *recordingTracker) Heartbeat(_ string, _ TrackerHeartbeat) error      { return nil }
func (r *recordingTracker) FetchCRL() ([]byte, error)                         { return nil, nil }

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

func (errClient) Ping(_ string) error             { return errors.New("unreachable") }
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
