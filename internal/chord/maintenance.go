package chord

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"chorddht/internal/logging"
)

// RunMaintenance launches all maintenance goroutines. They run until ctx is cancelled.
// For vnodes (VNodeIndex > 0), an initial stagger delay is applied to spread maintenance
// load across the physical host.
func (n *Node) RunMaintenance(ctx context.Context) {
	if n.options.VNodeIndex > 0 && n.options.MaxVNodes > 0 {
		baseInterval := n.getStabilizeInterval()
		offset := time.Duration(n.options.VNodeIndex) * baseInterval / time.Duration(n.options.MaxVNodes+1)
		jitterMax := n.options.VNodeMaintenanceJitter
		if jitterMax <= 0 {
			jitterMax = DefaultVNodeMaintenanceJitter
		}
		jitter := time.Duration(rand.Int63n(int64(jitterMax)))
		select {
		case <-ctx.Done():
			return
		case <-time.After(offset + jitter):
		}
	}
	go n.runModeManager(ctx)
	go n.runStabilizeLoop(ctx)
	go n.runFixFingersLoop(ctx)
	go n.runCheckPredecessorLoop(ctx)
	go n.runLatencyProbe(ctx)
	go n.runCacheCleanup(ctx)
}

// MaintenanceCycle is kept for backward compatibility with tests; the real work
// is now done by the individual goroutine loops launched by RunMaintenance.
func (n *Node) MaintenanceCycle() {
	status := n.Self().Status
	if status == StatusActive {
		n.CheckPredecessor()
		n.Stabilize()
		n.fixFingersBatch()
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

// ----- adaptive loop runners -----

func (n *Node) runModeManager(ctx context.Context) {
	window := n.options.TopologyChangeWindow
	if window <= 0 {
		window = DefaultTopologyChangeWindow
	}
	quietTimer := time.NewTimer(window)
	defer quietTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.topologyChangeCh:
			n.switchMode(ActiveMaintenance)
			n.mu.Lock()
			n.lastChangeTime = time.Now().UTC()
			n.mu.Unlock()
			if !quietTimer.Stop() {
				select {
				case <-quietTimer.C:
				default:
				}
			}
			quietTimer.Reset(window)
		case <-quietTimer.C:
			n.switchMode(QuietMaintenance)
			quietTimer.Reset(window)
		}
	}
}

func (n *Node) runStabilizeLoop(ctx context.Context) {
	for {
		n.mu.RLock()
		status := n.status
		n.mu.RUnlock()
		if status == StatusActive {
			n.Stabilize()
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

		timer := time.NewTimer(n.getStabilizeInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (n *Node) runFixFingersLoop(ctx context.Context) {
	for {
		n.mu.RLock()
		status := n.status
		n.mu.RUnlock()
		if status == StatusActive {
			n.fixFingersBatch()
		}

		timer := time.NewTimer(n.getFixFingersInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (n *Node) runCheckPredecessorLoop(ctx context.Context) {
	for {
		n.mu.RLock()
		status := n.status
		n.mu.RUnlock()
		if status == StatusActive {
			n.CheckPredecessor()
		}

		timer := time.NewTimer(n.getCheckPredecessorInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (n *Node) runLatencyProbe(ctx context.Context) {
	for {
		n.mu.RLock()
		status := n.status
		n.mu.RUnlock()
		if status == StatusActive {
			n.probeNeighbors()
		}

		timer := time.NewTimer(n.getLatencyProbeInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (n *Node) runCacheCleanup(ctx context.Context) {
	for {
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			n.rttCache.Cleanup()
			if n.routingCache != nil {
				n.routingCache.Cleanup()
			}
		}
	}
}

// ----- interval selectors -----

func (n *Node) getStabilizeInterval() time.Duration {
	n.mu.RLock()
	mode := n.maintenanceMode
	n.mu.RUnlock()
	if mode == ActiveMaintenance {
		if n.options.StabilizeActiveInterval > 0 {
			return n.options.StabilizeActiveInterval
		}
		return DefaultStabilizeActiveInterval
	}
	if n.options.StabilizeQuietInterval > 0 {
		return n.options.StabilizeQuietInterval
	}
	return DefaultStabilizeQuietInterval
}

func (n *Node) getFixFingersInterval() time.Duration {
	n.mu.RLock()
	mode := n.maintenanceMode
	n.mu.RUnlock()
	if mode == ActiveMaintenance {
		if n.options.FixFingersActiveInterval > 0 {
			return n.options.FixFingersActiveInterval
		}
		return DefaultFixFingersActiveInterval
	}
	if n.options.FixFingersQuietInterval > 0 {
		return n.options.FixFingersQuietInterval
	}
	return DefaultFixFingersQuietInterval
}

func (n *Node) getCheckPredecessorInterval() time.Duration {
	n.mu.RLock()
	mode := n.maintenanceMode
	n.mu.RUnlock()
	if mode == ActiveMaintenance {
		if n.options.CheckPredecessorActiveInterval > 0 {
			return n.options.CheckPredecessorActiveInterval
		}
		return DefaultCheckPredecessorActiveInterval
	}
	if n.options.CheckPredecessorQuietInterval > 0 {
		return n.options.CheckPredecessorQuietInterval
	}
	return DefaultCheckPredecessorQuietInterval
}

func (n *Node) getLatencyProbeInterval() time.Duration {
	n.mu.RLock()
	mode := n.maintenanceMode
	n.mu.RUnlock()
	if mode == ActiveMaintenance {
		if n.options.LatencyProbeIntervalActive > 0 {
			return n.options.LatencyProbeIntervalActive
		}
		return DefaultLatencyProbeActiveInterval
	}
	if n.options.LatencyProbeIntervalQuiet > 0 {
		return n.options.LatencyProbeIntervalQuiet
	}
	return DefaultLatencyProbeQuietInterval
}

func (n *Node) getBatchSize() int {
	n.mu.RLock()
	mode := n.maintenanceMode
	n.mu.RUnlock()
	if mode == ActiveMaintenance {
		if n.options.FixFingersBatchSizeActive > 0 {
			return n.options.FixFingersBatchSizeActive
		}
		return DefaultFixFingersBatchSizeActive
	}
	if n.options.FixFingersBatchSizeQuiet > 0 {
		return n.options.FixFingersBatchSizeQuiet
	}
	return DefaultFixFingersBatchSizeQuiet
}

// ----- CheckPredecessor with fast failure detection -----

func (n *Node) CheckPredecessor() {
	n.mu.RLock()
	pred := cloneNodePtr(n.predecessor)
	n.mu.RUnlock()
	if pred == nil || pred.NodeID == n.self.NodeID || n.client == nil {
		return
	}
	rttMs, err := n.client.PingWithLatency(pred.URI)
	if err != nil {
		// Fast failure: retry once after 2 seconds before declaring predecessor dead.
		time.Sleep(2 * time.Second)
		rttMs, err = n.client.PingWithLatency(pred.URI)
	}
	if err != nil {
		n.mu.Lock()
		if n.predecessor != nil && n.predecessor.NodeID == pred.NodeID {
			n.predecessor = nil
		}
		n.mu.Unlock()
		logging.Warnf("predecessor lost node_id=%s uri=%s error=%v", pred.NodeID, pred.URI, err)
		n.emitTopologyChange()
		return
	}
	n.rttCache.Record(pred.NodeID, rttMs)
	n.markSuccess(pred.NodeID)
}

// ----- Stabilize with fast successor switch and debounce -----

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
			n.mu.RLock()
			pred := cloneNodePtr(n.predecessor)
			n.mu.RUnlock()
			if pred == nil || pred.NodeID == self.NodeID || n.client == nil {
				n.mu.Lock()
				n.successor = self
				n.successorList = []NodeInfo{self}
				if currentSuccessor.NodeID != self.NodeID {
					n.status = StatusIsolated
					n.predecessor = nil
				}
				n.mu.Unlock()
				if currentSuccessor.NodeID != self.NodeID {
					logging.Warnf("node became isolated; no successor candidates reachable")
					n.emitTopologyChange()
				}
				return
			}
			successor := pred.Core()
			selfWithCert := self
			selfWithCert.Certificate = n.options.NodeCertificate
			_, _ = n.client.Notify(successor, NotifyRequest{Node: selfWithCert})
			list := []NodeInfo{successor}
			if resp, err := n.client.SuccessorList(successor); err == nil {
				list = append(list, resp.SuccessorList...)
			}
			n.mu.Lock()
			n.successor = successor
			n.successorList = n.mergeSuccessorListLocked(successor, list)
			n.fingers[0].Node = successor
			n.fingers[0].Status = FingerOK
			n.fingers[0].Valid = true
			n.mu.Unlock()
			logging.Infof("successor bootstrapped from predecessor successor=%s", successor.NodeID)
			n.emitTopologyChange()
			return
		}
		if n.client == nil {
			break
		}
		predResp, err := n.client.Predecessor(candidate)
		if err != nil {
			failures, evicted := n.markFailure(candidate.NodeID)
			if evicted {
				logging.Warnf("successor candidate evicted after failures node_id=%s uri=%s failures=%d error=%v", candidate.NodeID, candidate.URI, failures, err)
			} else {
				logging.Warnf("successor candidate failed node_id=%s uri=%s failures=%d error=%v", candidate.NodeID, candidate.URI, failures, err)
			}
			// Fast switch: immediately try next candidate instead of waiting next cycle.
			continue
		}
		n.markSuccess(candidate.NodeID)
		successor := candidate.Core()
		if predResp.Predecessor != nil && predResp.Predecessor.NodeID != self.NodeID && InRangeOpenOpen(predResp.Predecessor.NodeID, self.NodeID, candidate.NodeID) {
			successor = predResp.Predecessor.Core()
		}
		selfWithCert := self
		selfWithCert.Certificate = n.options.NodeCertificate
		_, _ = n.client.Notify(successor, NotifyRequest{Node: selfWithCert})
		list := []NodeInfo{successor}
		if resp, err := n.client.SuccessorList(successor); err == nil {
			list = append(list, resp.SuccessorList...)
		}
		n.mu.Lock()
		n.successor = successor
		n.successorList = n.mergeSuccessorListLocked(successor, list)
		n.fingers[0].Node = successor
		n.fingers[0].Status = FingerOK
		n.fingers[0].Valid = true
		topologyChanged := successor.NodeID != currentSuccessor.NodeID
		if topologyChanged {
			n.stabilizeDebounceCount++
		} else {
			n.stabilizeDebounceCount = 0
		}
		n.mu.Unlock()
		if topologyChanged {
			logging.Infof("successor changed from=%s to=%s", currentSuccessor.NodeID, successor.NodeID)
			n.emitTopologyChange()
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
	n.emitTopologyChange()
}

// ----- Batch parallel fix_fingers -----

// fixFingersBatch repairs k finger entries concurrently, in exponential-jump order.
func (n *Node) fixFingersBatch() {
	indices := n.nextKFingerIndices(n.getBatchSize())
	repairFingerIndices(n, indices)
}

// FixFingers is kept for backward compatibility (used by tests and MaintenanceCycle).
// New code should prefer fixFingersBatch.
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
		n.fingers[index].Valid = true
		n.fingers[index].LastVerified = time.Now().UTC()
	} else if n.fingers[index].Status == FingerOK {
		n.fingers[index].Status = FingerSuspicious
		n.fingers[index].Valid = false
	}
	n.nextFingerIndex = (index + 1) % DefaultM
}

// warmUpFingerTable concurrently fetches the top 32 finger table entries immediately after
// join, so the node has useful routing state before the first maintenance cycle runs.
func (n *Node) warmUpFingerTable() {
	count := DefaultM
	if count > 32 {
		count = 32
	}
	indices := make([]int, count)
	// Reuse exponential jump order for warm-up.
	for i := 0; i < count; i++ {
		indices[i] = exponentialJumpIndex(i)
	}
	repairFingerIndices(n, indices)
	logging.Infof("finger table warm-up complete count=%d", count)
}

// repairFingerIndices is the shared helper for fixFingersBatch and warmUpFingerTable.
func repairFingerIndices(n *Node, indices []int) {
	var wg sync.WaitGroup
	for _, i := range indices {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			n.mu.RLock()
			start := n.fingers[idx].Start
			n.mu.RUnlock()
			successor, err := n.LookupSuccessor(start)
			n.mu.Lock()
			if err == nil {
				n.fingers[idx].Node = successor.Core()
				n.fingers[idx].Status = FingerOK
				n.fingers[idx].Valid = true
				n.fingers[idx].LastVerified = time.Now().UTC()
			} else {
				n.fingers[idx].Valid = false
				if n.fingers[idx].Status == FingerOK {
					n.fingers[idx].Status = FingerSuspicious
				}
			}
			n.mu.Unlock()
		}(i)
	}
	wg.Wait()
}

// nextKFingerIndices returns the next k finger indices to repair, using exponential-jump
// priority order (high-index fingers first: 159, 140, 120, 100, ..., 1, 0).
func (n *Node) nextKFingerIndices(k int) []int {
	if k <= 0 {
		return nil
	}
	n.mu.Lock()
	start := n.nextFingerIndex
	n.nextFingerIndex = (start + k) % DefaultM
	n.mu.Unlock()

	out := make([]int, 0, k)
	for i := 0; i < k; i++ {
		// Map linear position to exponential-jump index.
		// Position 0 → finger 159, position 1 → 140, ... using decreasing powers of 2.
		pos := (start + i) % DefaultM
		idx := exponentialJumpIndex(pos)
		out = append(out, idx)
	}
	return out
}

// exponentialJumpIndex maps a sequential position (0..159) to a finger table index
// in exponential-jump priority order (159, 140, 120, 100, 80, 60, 40, 20, 10, 5, 2, 1, ...).
func exponentialJumpIndex(pos int) int {
	if pos >= DefaultM {
		pos = pos % DefaultM
	}
	// Pre-computed priority order: start at 159, subtract decreasing powers of 2.
	// After exhausting powers: distribute remaining indices linearly from 0.
	prioritized := []int{159, 140, 120, 100, 80, 60, 40, 20, 10, 5, 2, 1}
	nPriority := len(prioritized)
	if pos < nPriority {
		return prioritized[pos]
	}
	// Remaining indices not in prioritized: linear from 0, skipping already-used ones.
	remaining := make([]int, 0, DefaultM-nPriority)
	used := make(map[int]bool, nPriority)
	for _, v := range prioritized {
		used[v] = true
	}
	for i := 0; i < DefaultM; i++ {
		if !used[i] {
			remaining = append(remaining, i)
		}
	}
	idx := pos - nPriority
	if idx < len(remaining) {
		return remaining[idx]
	}
	return pos % DefaultM
}

// ----- Latency probe -----

func (n *Node) probeNeighbors() {
	if n.client == nil {
		return
	}
	n.mu.RLock()
	selfID := n.self.NodeID
	succ := n.successor
	pred := cloneNodePtr(n.predecessor)
	n.mu.RUnlock()

	probe := func(node NodeInfo) {
		if node.NodeID == "" || node.NodeID == selfID {
			return
		}
		rttMs, err := n.client.PingWithLatency(node.URI)
		if err == nil {
			n.rttCache.Record(node.NodeID, rttMs)
		}
	}
	probe(succ)
	if pred != nil {
		probe(*pred)
	}
}

// ----- HealthCheckRing -----

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
		rttMs, err := n.client.PingWithLatency(node.URI)
		if err != nil {
			failures, evicted := n.markFailure(node.NodeID)
			if evicted {
				logging.Warnf("ring node evicted after health check failures node_id=%s uri=%s failures=%d error=%v", node.NodeID, node.URI, failures, err)
			} else {
				logging.Warnf("ring node health check failed node_id=%s uri=%s failures=%d error=%v", node.NodeID, node.URI, failures, err)
			}
		} else {
			n.rttCache.Record(node.NodeID, rttMs)
			n.markSuccess(node.NodeID)
		}
	}
}

// ----- ReportToTracker -----

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
	maintenanceMode := n.maintenanceMode
	predecessorListSize := len(n.predecessorList)

	succList := make([]string, 0, len(n.successorList))
	for _, s := range n.successorList {
		if s.NodeID != "" {
			succList = append(succList, s.NodeID)
		}
	}
	predList := make([]string, 0, len(n.predecessorList))
	for _, p := range n.predecessorList {
		if p.NodeID != "" {
			predList = append(predList, p.NodeID)
		}
	}
	seen := map[string]struct{}{}
	fingerNodes := make([]string, 0)
	for _, f := range n.fingers {
		if f.Valid && f.Node.NodeID != "" && f.Node.NodeID != n.self.NodeID {
			if _, ok := seen[f.Node.NodeID]; !ok {
				seen[f.Node.NodeID] = struct{}{}
				fingerNodes = append(fingerNodes, f.Node.NodeID)
			}
		}
	}

	heartbeat := TrackerHeartbeat{
		Status:                status,
		SuccessorID:           successorID,
		PredecessorID:         predecessorID,
		SuccessorListSize:     len(n.successorList),
		SuccessorListCapacity: n.options.SuccessorListSize,
		FingerTableCoverage:   n.fingerCoverageLocked(),
		UptimeSeconds:         int64(time.Since(n.startedAt).Seconds()),
		MaintenanceCycles:     n.maintenanceCycles.Load(),
		CertExpiresAt:         n.options.NodeCertExpiresAt,
		Region:                n.region,
		MaintenanceMode:       maintenanceMode,
		PredecessorListSize:   predecessorListSize,
		SuccessorList:         succList,
		PredecessorList:       predList,
		FingerNodes:           fingerNodes,
	}
	n.mu.RUnlock()

	if n.rttCache != nil {
		heartbeat.RTTSamples = n.rttCache.Snapshot()
	}
	if n.routingCache != nil {
		heartbeat.CacheHits, heartbeat.CacheMisses, heartbeat.CacheSize = n.routingCache.Stats()
	}
	if err := n.tracker.Heartbeat(n.self.NodeID, heartbeat); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Code == ErrNodeNotFound {
			logging.Warnf("tracker heartbeat node not found, re-registering node_id=%s", n.self.NodeID)
			n.registerTracker()
			return
		}
		logging.Warnf("tracker heartbeat failed node_id=%s error=%v", n.self.NodeID, err)
		return
	}
	logging.Debugf("tracker heartbeat sent node_id=%s status=%s", n.self.NodeID, status)

	if n.options.OnCRLRefresh != nil {
		crlJSON, err := n.tracker.FetchCRL()
		if err != nil {
			logging.Debugf("crl fetch from tracker failed: %v", err)
			return
		}
		n.options.OnCRLRefresh(crlJSON)
	}
}

// ----- GracefulLeave -----

func (n *Node) GracefulLeave() {
	n.mu.Lock()
	n.status = StatusLeaving
	self := n.self.Core()
	successor := n.successor
	predecessor := cloneNodePtr(n.predecessor)
	n.mu.Unlock()
	if n.client != nil {
		if successor.NodeID != "" && successor.NodeID != self.NodeID {
			if err := n.client.Leave(successor, LeaveRequest{Role: "predecessor_leaving", NewPredecessor: predecessor}); err != nil {
				logging.Warnf("failed to notify successor during leave node_id=%s error=%v", successor.NodeID, err)
			} else {
				logging.Infof("notified successor during leave node_id=%s", successor.NodeID)
			}
		}
		if predecessor != nil && predecessor.NodeID != self.NodeID {
			if err := n.client.Leave(*predecessor, LeaveRequest{Role: "successor_leaving", NewSuccessor: &successor}); err != nil {
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

// ----- Multi-path ISOLATED recovery -----

func (n *Node) tryRecoverFromIsolation() {
	logging.Infof("attempting multi-path isolation recovery")

	// Path A: try nodes in successor list.
	if n.tryRecoverFromNodes(n.getSuccessorListCopy(), "successor_list") {
		return
	}
	// Path B: try nodes in predecessor list.
	if n.tryRecoverFromNodes(n.getPredecessorListCopy(), "predecessor_list") {
		return
	}
	// Path C: random finger table sample (up to 16 entries).
	if n.tryRecoverFromNodes(n.sampleFingers(16), "finger_sample") {
		return
	}
	// Path D: tracker seeds.
	if n.tracker != nil {
		seeds, err := n.tracker.Seeds(n.options.TrackerSeedCount, []string{n.self.NodeID})
		if err == nil && len(seeds) > 0 {
			logging.Infof("isolation recovery attempting rejoin via tracker seeds=%d", len(seeds))
			if err := n.JoinNetwork(seeds); err == nil {
				return
			}
		} else {
			logging.Warnf("isolation recovery tracker seed lookup failed: %v", err)
		}
	}
	// Path E: single-node fallback.
	logging.Infof("isolation recovery exhausted all paths; activating single-node ring")
	n.ActivateSingleNode()
}

func (n *Node) tryRecoverFromNodes(nodes []NodeInfo, source string) bool {
	if n.client == nil || len(nodes) == 0 {
		return false
	}
	selfID := n.self.NodeID
	for _, node := range nodes {
		if node.NodeID == "" || node.NodeID == selfID {
			continue
		}
		if err := n.client.Ping(node.URI); err == nil {
			logging.Infof("isolation recovery found live node source=%s node_id=%s", source, node.NodeID)
			_ = n.JoinNetwork([]NodeInfo{node})
			n.mu.RLock()
			recovered := n.status == StatusActive
			n.mu.RUnlock()
			if recovered {
				return true
			}
		}
	}
	return false
}

func (n *Node) getSuccessorListCopy() []NodeInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return cloneNodes(n.successorList)
}

func (n *Node) getPredecessorListCopy() []NodeInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return cloneNodes(n.predecessorList)
}

func (n *Node) sampleFingers(max int) []NodeInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()
	seen := map[string]bool{n.self.NodeID: true}
	out := make([]NodeInfo, 0, max)
	// Use stride to spread sample across ring.
	step := DefaultM / max
	if step < 1 {
		step = 1
	}
	for i := 0; i < DefaultM && len(out) < max; i += step {
		node := n.fingers[i].Node
		if node.NodeID != "" && !seen[node.NodeID] {
			seen[node.NodeID] = true
			out = append(out, node)
		}
	}
	return out
}

// ----- Failure tracking -----

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
				n.fingers[i].Valid = false
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
			n.fingers[i].Valid = false
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
