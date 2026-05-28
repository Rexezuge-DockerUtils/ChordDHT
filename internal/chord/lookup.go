package chord

import "net/http"

func (n *Node) HandleFindSuccessor(req FindSuccessorRequest) (FindSuccessorResponse, error) {
	if !ValidateID(req.ID) {
		return FindSuccessorResponse{}, NewAPIError(http.StatusBadRequest, ErrInvalidRequest, "id must be 40 lowercase hex characters")
	}
	if req.MaxHops <= 0 {
		req.MaxHops = n.options.MaxHops
	}
	if req.HopCount >= req.MaxHops {
		return FindSuccessorResponse{}, NewAPIError(http.StatusServiceUnavailable, ErrMaxHopsExceeded, "maximum hop count exceeded")
	}

	n.mu.RLock()
	self := n.self.Core()
	status := n.status
	successor := n.successor
	fingers := cloneFingers(n.fingers)
	successors := cloneNodes(n.successorList)
	n.mu.RUnlock()

	if status == StatusLeaving {
		return FindSuccessorResponse{}, NewAPIError(http.StatusServiceUnavailable, ErrNodeLeaving, "node is leaving")
	}
	if status == StatusIsolated {
		return FindSuccessorResponse{}, NewAPIError(http.StatusServiceUnavailable, ErrNodeIsolated, "node is isolated")
	}
	for _, visited := range req.VisitedNodes {
		if visited == self.NodeID {
			return FindSuccessorResponse{}, NewAPIError(http.StatusServiceUnavailable, ErrLoopDetected, "lookup loop detected")
		}
	}

	if successor.NodeID == self.NodeID || InRangeOpenClosed(req.ID, self.NodeID, successor.NodeID) {
		return FindSuccessorResponse{Found: true, Successor: &successor, HopCount: req.HopCount}, nil
	}

	next := closestPrecedingNode(self, req.ID, fingers, successors)
	if next.NodeID == self.NodeID {
		return FindSuccessorResponse{Found: true, Successor: &successor, HopCount: req.HopCount}, nil
	}
	return FindSuccessorResponse{Found: false, NextHop: &next, HopCount: req.HopCount + 1}, nil
}

func (n *Node) LookupSuccessor(id string) (NodeInfo, error) {
	if !ValidateID(id) {
		return NodeInfo{}, NewAPIError(http.StatusBadRequest, ErrInvalidRequest, "id must be 40 lowercase hex characters")
	}
	current := n.Self().Core()
	visited := make([]string, 0, n.options.MaxHops)
	for hop := 0; hop < n.options.MaxHops; hop++ {
		for _, nodeID := range visited {
			if nodeID == current.NodeID {
				return NodeInfo{}, NewAPIError(http.StatusServiceUnavailable, ErrLoopDetected, "lookup loop detected")
			}
		}
		req := FindSuccessorRequest{ID: id, HopCount: hop, MaxHops: n.options.MaxHops, VisitedNodes: cloneStrings(visited)}
		var resp FindSuccessorResponse
		var err error
		if current.NodeID == n.Self().NodeID {
			resp, err = n.HandleFindSuccessor(req)
		} else {
			if n.client == nil {
				return NodeInfo{}, NewAPIError(http.StatusServiceUnavailable, ErrUpstream, "peer client is not configured")
			}
			resp, err = n.client.FindSuccessor(current.URI, req)
		}
		if err != nil {
			return NodeInfo{}, err
		}
		if resp.Found {
			if resp.Successor == nil {
				return NodeInfo{}, NewAPIError(http.StatusServiceUnavailable, ErrUpstream, "find_successor response missing successor")
			}
			return resp.Successor.Core(), nil
		}
		if resp.NextHop == nil {
			return NodeInfo{}, NewAPIError(http.StatusServiceUnavailable, ErrUpstream, "find_successor response missing next_hop")
		}
		visited = append(visited, current.NodeID)
		current = resp.NextHop.Core()
	}
	return NodeInfo{}, NewAPIError(http.StatusServiceUnavailable, ErrMaxHopsExceeded, "maximum hop count exceeded")
}

func closestPrecedingNode(self NodeInfo, targetID string, fingers []FingerEntry, successors []NodeInfo) NodeInfo {
	if node := closestFromFingers(self, targetID, fingers, false); node.NodeID != "" {
		return node
	}
	if node := closestFromSuccessors(self, targetID, successors); node.NodeID != "" {
		return node
	}
	if node := closestFromFingers(self, targetID, fingers, true); node.NodeID != "" {
		return node
	}
	return self
}

func closestFromFingers(self NodeInfo, targetID string, fingers []FingerEntry, allowSuspicious bool) NodeInfo {
	for i := len(fingers) - 1; i >= 0; i-- {
		finger := fingers[i]
		if finger.Node.NodeID == "" || finger.Node.NodeID == self.NodeID {
			continue
		}
		if finger.Status == FingerSuspicious && !allowSuspicious {
			continue
		}
		if InRangeOpenOpen(finger.Node.NodeID, self.NodeID, targetID) {
			return finger.Node.Core()
		}
	}
	return NodeInfo{}
}

func closestFromSuccessors(self NodeInfo, targetID string, successors []NodeInfo) NodeInfo {
	for i := len(successors) - 1; i >= 0; i-- {
		node := successors[i]
		if node.NodeID != "" && node.NodeID != self.NodeID && InRangeOpenOpen(node.NodeID, self.NodeID, targetID) {
			return node.Core()
		}
	}
	return NodeInfo{}
}

func cloneStrings(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	return out
}
