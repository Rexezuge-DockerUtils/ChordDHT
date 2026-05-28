package chord

import (
	"context"
	"time"

	"chorddht/internal/logging"
)

func (n *Node) RunMaintenance(ctx context.Context) {
	ticker := time.NewTicker(n.options.MaintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.MaintenanceCycle()
		}
	}
}

func (n *Node) MaintenanceCycle() {
	status := n.Self().Status
	if status == StatusActive {
		n.CheckPredecessor()
		n.Stabilize()
		n.FixFingers()
		n.HealthCheckRing()
		n.ReportToTracker()
	} else if status == StatusIsolated {
		n.tryRecoverFromIsolation()
	}
	n.mu.Lock()
	now := time.Now().UTC()
	n.lastMaintenanceAt = &now
	n.mu.Unlock()
	n.maintenanceCycles.Add(1)
}

func (n *Node) CheckPredecessor() {
	n.mu.RLock()
	pred := cloneNodePtr(n.predecessor)
	n.mu.RUnlock()
	if pred == nil || pred.NodeID == n.self.NodeID || n.client == nil {
		return
	}
	if err := n.client.Ping(pred.URI); err != nil {
		n.mu.Lock()
		if n.predecessor != nil && n.predecessor.NodeID == pred.NodeID {
			n.predecessor = nil
		}
		n.mu.Unlock()
		logging.Warnf("predecessor lost node_id=%s uri=%s error=%v", pred.NodeID, pred.URI, err)
		return
	}
	n.markSuccess(pred.NodeID)
}

func (n *Node) Stabilize() {
	n.mu.RLock()
	self := n.self.Core()
	status := n.status
	currentSuccessor := n.successor
	candidates := append([]NodeInfo{n.successor}, n.successorList...)
	n.mu.RUnlock()
	if status != StatusActive {
		return
	}
	candidates = dedupeNodes(candidates, "")
	for _, candidate := range candidates {
		if candidate.NodeID == self.NodeID {
			// If we have a predecessor, use it to bootstrap our successor pointer.
			// Per Chord: InRangeOpenOpen(pred, self, self) == true for any pred != self,
			// so the predecessor is the correct next successor candidate.
			// Without this, a seed node's successor stays "self" forever after others join.
			n.mu.RLock()
			pred := cloneNodePtr(n.predecessor)
			n.mu.RUnlock()
			if pred == nil || pred.NodeID == self.NodeID || n.client == nil {
				n.mu.Lock()
				n.successor = self
				n.successorList = []NodeInfo{self}
				n.mu.Unlock()
				return
			}
			successor := pred.Core()
			_, _ = n.client.Notify(successor.URI, NotifyRequest{Node: self})
			list := []NodeInfo{successor}
			if resp, err := n.client.SuccessorList(successor.URI); err == nil {
				list = append(list, resp.SuccessorList...)
			}
			n.mu.Lock()
			n.successor = successor
			n.successorList = n.mergeSuccessorListLocked(successor, list)
			n.fingers[0].Node = successor
			n.fingers[0].Status = FingerOK
			n.mu.Unlock()
			logging.Infof("successor bootstrapped from predecessor successor=%s", successor.NodeID)
			return
		}
		if n.client == nil {
			break
		}
		predResp, err := n.client.Predecessor(candidate.URI)
		if err != nil {
			failures, evicted := n.markFailure(candidate.NodeID)
			if evicted {
				logging.Warnf("successor candidate evicted after failures node_id=%s uri=%s failures=%d error=%v", candidate.NodeID, candidate.URI, failures, err)
			} else {
				logging.Warnf("successor candidate failed node_id=%s uri=%s failures=%d error=%v", candidate.NodeID, candidate.URI, failures, err)
			}
			continue
		}
		n.markSuccess(candidate.NodeID)
		successor := candidate.Core()
		if predResp.Predecessor != nil && predResp.Predecessor.NodeID != self.NodeID && InRangeOpenOpen(predResp.Predecessor.NodeID, self.NodeID, candidate.NodeID) {
			successor = predResp.Predecessor.Core()
		}
		_, _ = n.client.Notify(successor.URI, NotifyRequest{Node: self})
		list := []NodeInfo{successor}
		if resp, err := n.client.SuccessorList(successor.URI); err == nil {
			list = append(list, resp.SuccessorList...)
		}
		n.mu.Lock()
		n.successor = successor
		n.successorList = n.mergeSuccessorListLocked(successor, list)
		n.fingers[0].Node = successor
		n.fingers[0].Status = FingerOK
		n.mu.Unlock()
		if successor.NodeID != currentSuccessor.NodeID {
			logging.Infof("successor changed from=%s to=%s", currentSuccessor.NodeID, successor.NodeID)
		}
		return
	}
	n.mu.Lock()
	n.status = StatusIsolated
	n.predecessor = nil
	n.successor = self
	n.successorList = []NodeInfo{self}
	n.mu.Unlock()
	logging.Warnf("node became isolated; no successor candidates reachable")
}

func (n *Node) FixFingers() {
	n.mu.RLock()
	index := n.nextFingerIndex
	start := n.fingers[index].Start
	n.mu.RUnlock()
	successor, err := n.LookupSuccessor(start)
	n.mu.Lock()
	defer n.mu.Unlock()
	if err == nil {
		n.fingers[index].Node = successor.Core()
		n.fingers[index].Status = FingerOK
	} else if n.fingers[index].Status == FingerOK {
		n.fingers[index].Status = FingerSuspicious
	}
	n.nextFingerIndex = (index + 1) % DefaultM
}

func (n *Node) HealthCheckRing() {
	n.mu.RLock()
	successors := cloneNodes(n.successorList)
	selfID := n.self.NodeID
	n.mu.RUnlock()
	if n.client == nil {
		return
	}
	for _, node := range successors {
		if node.NodeID == selfID {
			continue
		}
		if err := n.client.Ping(node.URI); err != nil {
			failures, evicted := n.markFailure(node.NodeID)
			if evicted {
				logging.Warnf("ring node evicted after health check failures node_id=%s uri=%s failures=%d error=%v", node.NodeID, node.URI, failures, err)
			} else {
				logging.Warnf("ring node health check failed node_id=%s uri=%s failures=%d error=%v", node.NodeID, node.URI, failures, err)
			}
		} else {
			n.markSuccess(node.NodeID)
		}
	}
}

func (n *Node) ReportToTracker() {
	if n.tracker == nil {
		return
	}
	n.mu.RLock()
	status := n.status
	successorID := stringPtrIfNotEmpty(n.successor.NodeID)
	var predecessorID *string
	if n.predecessor != nil {
		predecessorID = stringPtrIfNotEmpty(n.predecessor.NodeID)
	}
	heartbeat := TrackerHeartbeat{
		Status:              status,
		SuccessorID:         successorID,
		PredecessorID:       predecessorID,
		SuccessorListSize:   len(n.successorList),
		FingerTableCoverage: n.fingerCoverageLocked(),
		UptimeSeconds:       int64(time.Since(n.startedAt).Seconds()),
		MaintenanceCycles:   n.maintenanceCycles.Load(),
	}
	n.mu.RUnlock()
	if err := n.tracker.Heartbeat(n.self.NodeID, heartbeat); err != nil {
		logging.Warnf("tracker heartbeat failed node_id=%s error=%v", n.self.NodeID, err)
		return
	}
	logging.Debugf("tracker heartbeat sent node_id=%s status=%s", n.self.NodeID, status)
}

func (n *Node) GracefulLeave() {
	n.mu.Lock()
	n.status = StatusLeaving
	self := n.self.Core()
	successor := n.successor
	predecessor := cloneNodePtr(n.predecessor)
	n.mu.Unlock()
	if n.client != nil {
		if successor.NodeID != "" && successor.NodeID != self.NodeID {
			if err := n.client.Leave(successor.URI, LeaveRequest{Role: "predecessor_leaving", NewPredecessor: predecessor}); err != nil {
				logging.Warnf("failed to notify successor during leave node_id=%s error=%v", successor.NodeID, err)
			} else {
				logging.Infof("notified successor during leave node_id=%s", successor.NodeID)
			}
		}
		if predecessor != nil && predecessor.NodeID != self.NodeID {
			if err := n.client.Leave(predecessor.URI, LeaveRequest{Role: "successor_leaving", NewSuccessor: &successor}); err != nil {
				logging.Warnf("failed to notify predecessor during leave node_id=%s error=%v", predecessor.NodeID, err)
			} else {
				logging.Infof("notified predecessor during leave node_id=%s", predecessor.NodeID)
			}
		}
	}
	if n.tracker != nil {
		if err := n.tracker.Deregister(self.NodeID); err != nil {
			logging.Warnf("tracker deregistration failed node_id=%s error=%v", self.NodeID, err)
		} else {
			logging.Infof("deregistered node from tracker node_id=%s", self.NodeID)
		}
	}
}

func (n *Node) tryRecoverFromIsolation() {
	if n.tracker == nil {
		logging.Infof("recovering from isolation without tracker; activating single-node ring")
		n.ActivateSingleNode()
		return
	}
	seeds, err := n.tracker.Seeds(n.options.TrackerSeedCount, []string{n.self.NodeID})
	if err != nil || len(seeds) == 0 {
		if err != nil {
			logging.Warnf("isolation recovery seed lookup failed: %v", err)
		} else {
			logging.Warnf("isolation recovery found no seeds")
		}
		n.ActivateSingleNode()
		return
	}
	logging.Infof("attempting isolation recovery with seeds=%d", len(seeds))
	_ = n.JoinNetwork(seeds)
}

func (n *Node) markFailure(nodeID string) (int, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.failures[nodeID]++
	failures := n.failures[nodeID]
	if n.failures[nodeID] >= n.options.FailedThreshold && n.options.FailedThreshold > 0 {
		for i := range n.fingers {
			if n.fingers[i].Node.NodeID == nodeID {
				n.fingers[i].Node = n.self.Core()
				n.fingers[i].Status = FingerUnknown
			}
		}
		filtered := n.successorList[:0]
		for _, node := range n.successorList {
			if node.NodeID != nodeID {
				filtered = append(filtered, node)
			}
		}
		n.successorList = filtered
		return failures, true
	}
	for i := range n.fingers {
		if n.fingers[i].Node.NodeID == nodeID {
			n.fingers[i].Status = FingerSuspicious
		}
	}
	return failures, false
}

func (n *Node) markSuccess(nodeID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.failures, nodeID)
	for i := range n.fingers {
		if n.fingers[i].Node.NodeID == nodeID {
			n.fingers[i].Status = FingerOK
		}
	}
}

func (n *Node) fingerCoverageLocked() float64 {
	if len(n.fingers) == 0 {
		return 0
	}
	ok := 0
	for _, finger := range n.fingers {
		if finger.Status == FingerOK {
			ok++
		}
	}
	return float64(ok) / float64(len(n.fingers))
}

func stringPtrIfNotEmpty(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}
