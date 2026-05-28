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
	if err := ValidateNodeInfo(req.Node); err != nil {
		return NotifyResponse{}, NewAPIError(http.StatusBadRequest, ErrInvalidRequest, err.Error())
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.status == StatusLeaving {
		return NotifyResponse{}, NewAPIError(http.StatusServiceUnavailable, ErrNodeLeaving, "node is leaving")
	}
	accepted := false
	candidate := req.Node.Core()
	var previousPredecessor string
	if n.predecessor != nil {
		previousPredecessor = n.predecessor.NodeID
	}
	if candidate.NodeID != n.self.NodeID && (n.predecessor == nil || InRangeOpenOpen(candidate.NodeID, n.predecessor.NodeID, n.self.NodeID)) {
		n.predecessor = &candidate
		accepted = true
	}
	if accepted {
		logging.Infof("accepted predecessor notification from=%s previous=%s", candidate.NodeID, previousPredecessor)
	} else {
		logging.Debugf("rejected predecessor notification from=%s current=%s", candidate.NodeID, previousPredecessor)
	}
	return NotifyResponse{Accepted: accepted, Predecessor: cloneNodePtr(n.predecessor)}, nil
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
	resp, err := n.client.SuccessorList(successor.URI)
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
	seen := map[string]bool{n.self.NodeID: true}
	out := make([]NodeInfo, 0, limit)
	for _, candidate := range append([]NodeInfo{successor.Core()}, remote...) {
		if candidate.NodeID == "" || seen[candidate.NodeID] {
			continue
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
