package chord

import "testing"

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
