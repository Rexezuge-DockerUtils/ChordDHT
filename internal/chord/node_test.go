package chord

import (
	"errors"
	"testing"
)

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

func (errClient) Ping(_ string) error                                              { return errors.New("unreachable") }
func (errClient) PingWithLatency(_ string) (int64, error)                         { return 0, errors.New("unreachable") }
func (errClient) FindSuccessor(_ string, _ FindSuccessorRequest) (FindSuccessorResponse, error) {
	return FindSuccessorResponse{}, errors.New("unreachable")
}
func (errClient) Join(_ string, _ JoinRequest) (JoinResponse, error) {
	return JoinResponse{}, errors.New("unreachable")
}
func (errClient) Notify(_ string, _ NotifyRequest) (NotifyResponse, error) {
	return NotifyResponse{}, errors.New("unreachable")
}
func (errClient) Predecessor(_ string) (PredecessorResponse, error) {
	return PredecessorResponse{}, errors.New("unreachable")
}
func (errClient) SuccessorList(_ string) (SuccessorListResponse, error) {
	return SuccessorListResponse{}, errors.New("unreachable")
}
func (errClient) Leave(_ string, _ LeaveRequest) error { return errors.New("unreachable") }
func (errClient) RTT(_ string) (RTTResponse, error)    { return RTTResponse{}, errors.New("unreachable") }

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
