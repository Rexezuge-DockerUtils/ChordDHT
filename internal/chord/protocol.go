package chord

import (
	"net/http"

	"chorddht/internal/logging"
)

func (n *Node) HandleJoin(req JoinRequest) (JoinResponse, error) {
	if err := ValidateNodeInfo(req.Node); err != nil {
		return JoinResponse{}, NewAPIError(http.StatusBadRequest, ErrInvalidRequest, err.Error())
	}
	n.mu.RLock()
	status := n.status
	selfID := n.self.NodeID
	n.mu.RUnlock()
	if status == StatusLeaving {
		return JoinResponse{}, NewAPIError(http.StatusServiceUnavailable, ErrNodeLeaving, "node is leaving")
	}
	if status == StatusIsolated {
		return JoinResponse{}, NewAPIError(http.StatusServiceUnavailable, ErrNodeIsolated, "node is isolated")
	}
	if req.Node.NodeID == selfID {
		return JoinResponse{}, NewAPIError(http.StatusConflict, ErrIDCollision, "node id collides with this node")
	}
	successor, err := n.LookupSuccessor(req.Node.NodeID)
	if err != nil {
		return JoinResponse{}, err
	}
	logging.Infof("accepted join request node_id=%s successor=%s", req.Node.NodeID, successor.NodeID)
	return JoinResponse{Successor: successor.Core(), SuccessorList: n.successorListFor(successor)}, nil
}

func (n *Node) HandleNotify(req NotifyRequest) (NotifyResponse, error) {
	return n.HandleRectify(req)
}

func (n *Node) HandleRectify(req RectifyRequest) (RectifyResponse, error) {
	if err := ValidateNodeInfo(req.Node); err != nil {
		return RectifyResponse{}, NewAPIError(http.StatusBadRequest, ErrInvalidRequest, err.Error())
	}
	candidate := req.Node.Core()
	n.mu.RLock()
	status := n.status
	selfID := n.self.NodeID
	currentPredecessor := cloneNodePtr(n.predecessor)
	n.mu.RUnlock()
	if status == StatusLeaving {
		return RectifyResponse{}, NewAPIError(http.StatusServiceUnavailable, ErrNodeLeaving, "node is leaving")
	}
	if status == StatusJoining {
		return RectifyResponse{}, NewAPIError(http.StatusServiceUnavailable, ErrNodeJoining, "node is joining")
	}
	if candidate.NodeID == selfID {
		return RectifyResponse{}, NewAPIError(http.StatusBadRequest, ErrInvalidRequest, "candidate cannot be this node")
	}

	predecessorAlive := true
	if currentPredecessor != nil && currentPredecessor.NodeID != selfID && n.client != nil {
		predecessorAlive = n.pingLiveness(*currentPredecessor)
	}

	n.mu.Lock()
	accepted := false
	var previousPredecessor string
	if n.predecessor != nil {
		previousPredecessor = n.predecessor.NodeID
	}
	if n.predecessor == nil || (currentPredecessor != nil && n.predecessor.NodeID == currentPredecessor.NodeID && !predecessorAlive) || InRangeOpenOpen(candidate.NodeID, n.predecessor.NodeID, n.self.NodeID) {
		// Update predecessor chain: shift existing predecessor to [1], new candidate to [0].
		p := n.options.PredecessorListSize
		if p <= 0 {
			p = DefaultPredecessorListSize
		}
		newList := make([]NodeInfo, 0, p)
		newList = append(newList, candidate)
		if n.predecessor != nil && len(newList) < p {
			newList = append(newList, *n.predecessor)
		}
		n.predecessorList = newList
		n.predecessor = &candidate
		accepted = true
	}
	currentPred := cloneNodePtr(n.predecessor)
	n.mu.Unlock()

	if accepted {
		logging.Infof("accepted predecessor notification from=%s previous=%s", candidate.NodeID, previousPredecessor)
		n.emitTopologyChange()
		// Async: fetch candidate's predecessor to fill predecessorList[1].
		if n.client != nil {
			go func() {
				resp, err := n.client.Predecessor(candidate)
				if err == nil && resp.Predecessor != nil && resp.Predecessor.NodeID != n.self.NodeID {
					n.mu.Lock()
					p := n.options.PredecessorListSize
					if p <= 0 {
						p = DefaultPredecessorListSize
					}
					if len(n.predecessorList) >= 1 && n.predecessorList[0].NodeID == candidate.NodeID {
						if len(n.predecessorList) < p {
							n.predecessorList = append(n.predecessorList, resp.Predecessor.Core())
						} else if len(n.predecessorList) >= 2 {
							n.predecessorList[1] = resp.Predecessor.Core()
						}
					}
					n.mu.Unlock()
				}
			}()
		}
	} else {
		logging.Debugf("rejected predecessor rectify from=%s current=%s", candidate.NodeID, previousPredecessor)
	}
	return RectifyResponse{Accepted: accepted, Predecessor: currentPred}, nil
}

func (n *Node) HandleLeave(req LeaveRequest) (LeaveResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	switch req.Role {
	case "predecessor_leaving":
		previous := ""
		if n.predecessor != nil {
			previous = n.predecessor.NodeID
		}
		if req.NewPredecessor != nil {
			n.predecessor = cloneNodePtr(req.NewPredecessor)
		} else {
			n.predecessor = nil
		}
		logging.Infof("handled predecessor leave previous=%s", previous)
	case "successor_leaving":
		previous := n.successor.NodeID
		if req.NewSuccessor != nil {
			n.successor = req.NewSuccessor.Core()
			n.successorList = n.mergeSuccessorListLocked(n.successor, n.successorList)
		}
		logging.Infof("handled successor leave previous=%s current=%s", previous, n.successor.NodeID)
	default:
		return LeaveResponse{}, NewAPIError(http.StatusBadRequest, ErrInvalidRequest, "unknown leave role")
	}
	return LeaveResponse{Acknowledged: true}, nil
}

func (n *Node) successorListFor(successor NodeInfo) []NodeInfo {
	if successor.NodeID == n.Self().NodeID {
		return n.SuccessorList().SuccessorList
	}
	if n.client == nil {
		return []NodeInfo{successor.Core()}
	}
	if n.options.StabilizeAtomicState {
		state, err := n.client.State(successor)
		if err == nil {
			return append([]NodeInfo{successor.Core()}, state.SuccessorList...)
		}
	}
	resp, err := n.client.SuccessorList(successor)
	if err != nil {
		return []NodeInfo{successor.Core()}
	}
	return append([]NodeInfo{successor.Core()}, resp.SuccessorList...)
}

func (n *Node) mergeSuccessorListLocked(successor NodeInfo, remote []NodeInfo) []NodeInfo {
	limit := n.options.SuccessorListSize
	if limit <= 0 {
		limit = DefaultSuccessorListSize
	}
	siblingCap := n.options.SuccessorListSiblingCap
	if siblingCap <= 0 {
		siblingCap = DefaultSuccessorListSiblingCap
	}
	maxFromAnchor := int(float64(limit) * siblingCap)
	if maxFromAnchor < 1 {
		maxFromAnchor = 1
	}

	seen := map[string]bool{n.self.NodeID: true}
	anchorCount := map[string]int{}
	out := make([]NodeInfo, 0, limit)
	for _, candidate := range append([]NodeInfo{successor.Core()}, remote...) {
		if candidate.NodeID == "" || seen[candidate.NodeID] {
			continue
		}
		// Sibling diversity: limit entries from the same anchor.
		if candidate.AnchorID != "" && anchorCount[candidate.AnchorID] >= maxFromAnchor {
			continue
		}
		if candidate.AnchorID != "" {
			anchorCount[candidate.AnchorID]++
		}
		seen[candidate.NodeID] = true
		out = append(out, candidate.Core())
		if len(out) == limit {
			break
		}
	}
	if len(out) == 0 {
		out = append(out, n.self.Core())
	}
	return out
}
