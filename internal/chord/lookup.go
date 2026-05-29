package chord

import (
	"context"
	"math/big"
	"net/http"
	"sync"

	"chorddht/internal/logging"
)

// ensure math/big is used for the ID proximity calculation in closestPrecedingNode.
var _ = new(big.Int)

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

	// Routing cache check.
	if n.routingCache != nil {
		if cached, ok := n.routingCache.Get(req.ID); ok {
			resp := FindSuccessorResponse{
				Found:     true,
				Successor: &cached,
				HopCount:  req.HopCount,
				Cached:    true,
				Piggyback: n.buildPiggyback(),
			}
			logging.Debugf("find_successor cache hit target=%s successor=%s", req.ID, cached.NodeID)
			return resp, nil
		}
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
		logging.Debugf("find_successor resolved locally target=%s successor=%s hop=%d", req.ID, successor.NodeID, req.HopCount)
		resp := FindSuccessorResponse{
			Found:     true,
			Successor: &successor,
			HopCount:  req.HopCount,
			Piggyback: n.buildPiggyback(),
		}
		if n.routingCache != nil {
			n.routingCache.Put(req.ID, self.NodeID, successor)
		}
		return resp, nil
	}

	next := n.closestPrecedingNode(self, req.ID, fingers, successors)
	if next.NodeID == self.NodeID {
		logging.Debugf("find_successor falling back to successor target=%s successor=%s hop=%d", req.ID, successor.NodeID, req.HopCount)
		resp := FindSuccessorResponse{
			Found:     true,
			Successor: &successor,
			HopCount:  req.HopCount,
			Piggyback: n.buildPiggyback(),
		}
		return resp, nil
	}
	logging.Debugf("find_successor forwarding target=%s next_hop=%s hop=%d", req.ID, next.NodeID, req.HopCount+1)
	return FindSuccessorResponse{Found: false, NextHop: &next, HopCount: req.HopCount + 1, Piggyback: n.buildPiggyback()}, nil
}

func (n *Node) LookupSuccessor(id string) (NodeInfo, error) {
	if !ValidateID(id) {
		return NodeInfo{}, NewAPIError(http.StatusBadRequest, ErrInvalidRequest, "id must be 40 lowercase hex characters")
	}

	// Check routing cache first.
	if n.routingCache != nil {
		if cached, ok := n.routingCache.Get(id); ok {
			logging.Debugf("lookup cache hit target=%s successor=%s", id, cached.NodeID)
			return cached, nil
		}
	}

	if n.options.ParallelLookupEnabled && n.options.ParallelLookupCandidates > 1 {
		return n.lookupParallel(id)
	}
	return n.lookupIterative(id)
}

func (n *Node) lookupIterative(id string) (NodeInfo, error) {
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
			result := resp.Successor.Core()
			logging.Debugf("lookup resolved target=%s successor=%s hops=%d", id, result.NodeID, hop)
			// Store in routing cache; current is the predecessor (the node that returned Found).
			if n.routingCache != nil && !resp.Cached {
				n.routingCache.Put(id, current.NodeID, result)
			}
			// Merge any piggyback RTT hints.
			n.applyPiggyback(resp.Piggyback)
			return result, nil
		}
		if resp.NextHop == nil {
			return NodeInfo{}, NewAPIError(http.StatusServiceUnavailable, ErrUpstream, "find_successor response missing next_hop")
		}
		n.applyPiggyback(resp.Piggyback)
		visited = append(visited, current.NodeID)
		current = resp.NextHop.Core()
	}
	return NodeInfo{}, NewAPIError(http.StatusServiceUnavailable, ErrMaxHopsExceeded, "maximum hop count exceeded")
}

// lookupParallel fires concurrent find_successor RPCs to the top-k closest-preceding nodes.
func (n *Node) lookupParallel(id string) (NodeInfo, error) {
	n.mu.RLock()
	self := n.self.Core()
	fingers := cloneFingers(n.fingers)
	successors := cloneNodes(n.successorList)
	n.mu.RUnlock()

	k := n.options.ParallelLookupCandidates
	if k <= 0 {
		k = 3
	}
	candidates := n.topKClosestPreceding(self, id, fingers, successors, k)
	if len(candidates) == 0 {
		return n.lookupIterative(id)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		node NodeInfo
		err  error
	}
	ch := make(chan result, len(candidates))

	var wg sync.WaitGroup
	for _, c := range candidates {
		wg.Add(1)
		go func(peer NodeInfo) {
			defer wg.Done()
			if n.client == nil {
				ch <- result{err: NewAPIError(http.StatusServiceUnavailable, ErrUpstream, "no client")}
				return
			}
			req := FindSuccessorRequest{
				ID:      id,
				MaxHops: n.options.MaxHops,
			}
			resp, err := n.client.FindSuccessor(peer.URI, req)
			if err != nil || !resp.Found || resp.Successor == nil {
				select {
				case ch <- result{err: err}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case ch <- result{node: resp.Successor.Core()}:
			case <-ctx.Done():
			}
		}(c)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for res := range ch {
		if res.err == nil && res.node.NodeID != "" {
			cancel()
			if n.routingCache != nil {
				n.routingCache.Put(id, self.NodeID, res.node)
			}
			return res.node, nil
		}
	}
	// All parallel attempts failed; fall back to iterative.
	return n.lookupIterative(id)
}

// ----- Latency-aware closest-preceding-node selection -----

func (n *Node) closestPrecedingNode(self NodeInfo, targetID string, fingers []FingerEntry, successors []NodeInfo) NodeInfo {
	candidates := collectCandidates(self, targetID, fingers, successors)
	if len(candidates) == 0 {
		return self
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	wID := n.options.LatencyWeightID
	wRTT := n.options.LatencyWeightRTT
	wRegion := n.options.LatencyWeightRegion
	// Fall back to pure ID distance when weights are zero (or RTT data unavailable).
	if wID == 0 {
		wID, wRTT, wRegion = 1.0, 0.0, 0.0
	}

	targetBig, err := idToBig(targetID)
	if err != nil {
		return candidates[0]
	}
	max2m := new(big.Int).Lsh(big.NewInt(1), DefaultM)

	best := candidates[0]
	bestScore := -1.0
	for _, c := range candidates {
		score := 0.0
		// ID proximity score: 1 - (target - node) / 2^m (modular arithmetic).
		cBig, err := idToBig(c.NodeID)
		if err != nil {
			continue
		}
		diff := new(big.Int).Sub(targetBig, cBig)
		if diff.Sign() < 0 {
			diff.Add(diff, max2m)
		}
		idProx := 1.0 - float64(diff.BitLen())/float64(DefaultM+1)
		score += wID * idProx

		if wRTT > 0 {
			if rttMs, ok := n.rttCache.Get(c.NodeID); ok {
				rttScore := 1.0 / (1.0 + float64(rttMs)/100.0)
				score += wRTT * rttScore
			}
		}

		if wRegion > 0 && c.Region != "" && c.Region == n.region {
			score += wRegion
		}

		if score > bestScore {
			bestScore = score
			best = c
		}
	}
	return best
}

// topKClosestPreceding returns up to k candidates from finger table and successors that
// are in (self.NodeID, targetID) in the ring ID space.
func (n *Node) topKClosestPreceding(self NodeInfo, targetID string, fingers []FingerEntry, successors []NodeInfo, k int) []NodeInfo {
	candidates := collectCandidates(self, targetID, fingers, successors)
	if len(candidates) <= k {
		return candidates
	}
	return candidates[:k]
}

// collectCandidates gathers all eligible routing candidates from fingers and successor list.
func collectCandidates(self NodeInfo, targetID string, fingers []FingerEntry, successors []NodeInfo) []NodeInfo {
	seen := map[string]bool{self.NodeID: true}
	var out []NodeInfo

	for i := len(fingers) - 1; i >= 0; i-- {
		f := fingers[i]
		if f.Node.NodeID == "" || seen[f.Node.NodeID] {
			continue
		}
		if f.Status == FingerSuspicious || !f.Valid {
			continue
		}
		if InRangeOpenOpen(f.Node.NodeID, self.NodeID, targetID) {
			seen[f.Node.NodeID] = true
			out = append(out, f.Node)
		}
	}
	// Also consider suspicious fingers as fallback.
	if len(out) == 0 {
		for i := len(fingers) - 1; i >= 0; i-- {
			f := fingers[i]
			if f.Node.NodeID == "" || seen[f.Node.NodeID] {
				continue
			}
			if InRangeOpenOpen(f.Node.NodeID, self.NodeID, targetID) {
				seen[f.Node.NodeID] = true
				out = append(out, f.Node)
			}
		}
	}
	for i := len(successors) - 1; i >= 0; i-- {
		node := successors[i]
		if node.NodeID == "" || seen[node.NodeID] {
			continue
		}
		if InRangeOpenOpen(node.NodeID, self.NodeID, targetID) {
			seen[node.NodeID] = true
			out = append(out, node)
		}
	}
	return out
}

// applyPiggyback merges piggyback RTT hints into the local RTT cache.
func (n *Node) applyPiggyback(pb *PiggybackData) {
	if pb == nil || n.rttCache == nil {
		return
	}
	for nodeID, rttMs := range pb.SenderRTTHints {
		if ValidateID(nodeID) {
			n.rttCache.Record(nodeID, rttMs)
		}
	}
}

func cloneStrings(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	return out
}
