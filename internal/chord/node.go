package chord

import (
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"chorddht/internal/logging"
)

type Options struct {
	SuccessorListSize   int
	MaintenanceInterval time.Duration
	MaxHops             int
	SuspiciousThreshold int
	FailedThreshold     int
	TrackerSeedCount    int
	// NodeCertificate holds the node's own certificate as raw JSON.
	// When set, it is attached to JoinRequest, NotifyRequest, and tracker registration.
	NodeCertificate json.RawMessage
	// NodeCertExpiresAt is the Unix timestamp when the node's certificate expires.
	// Sent to tracker in heartbeat as cert_expires_at.
	NodeCertExpiresAt *int64
	// OnCRLRefresh is called after each successful tracker heartbeat with the raw CRL JSON
	// (or nil if no CRL is available). May be nil.
	OnCRLRefresh func(crlJSON []byte)
}

func DefaultOptions() Options {
	return Options{
		SuccessorListSize:   DefaultSuccessorListSize,
		MaintenanceInterval: DefaultMaintenanceInterval,
		MaxHops:             DefaultMaxHops,
		SuspiciousThreshold: DefaultSuspiciousThreshold,
		FailedThreshold:     DefaultFailedThreshold,
		TrackerSeedCount:    DefaultTrackerSeedCount,
	}
}

type Node struct {
	mu                sync.RWMutex
	self              NodeInfo
	predecessor       *NodeInfo
	successor         NodeInfo
	successorList     []NodeInfo
	fingers           []FingerEntry
	status            Status
	joinedAt          time.Time
	startedAt         time.Time
	lastMaintenanceAt *time.Time
	nextFingerIndex   int
	maintenanceCycles atomic.Uint64
	client            PeerClient
	tracker           TrackerClient
	options           Options
	failures          map[string]int
}

func NewNode(uri string, opts Options, client PeerClient, tracker TrackerClient) (*Node, error) {
	self, err := NewNodeInfoFromURI(uri)
	if err != nil {
		return nil, err
	}
	if opts.SuccessorListSize <= 0 {
		opts.SuccessorListSize = DefaultSuccessorListSize
	}
	if opts.MaintenanceInterval <= 0 {
		opts.MaintenanceInterval = DefaultMaintenanceInterval
	}
	if opts.MaxHops <= 0 {
		opts.MaxHops = DefaultMaxHops
	}
	if opts.TrackerSeedCount <= 0 {
		opts.TrackerSeedCount = DefaultTrackerSeedCount
	}

	fingers := make([]FingerEntry, DefaultM)
	for i := range fingers {
		start, err := FingerStart(self.NodeID, i)
		if err != nil {
			return nil, err
		}
		fingers[i] = FingerEntry{Index: i, Start: start, Node: self.Core(), Status: FingerUnknown}
	}

	n := &Node{
		self:          self,
		successor:     self.Core(),
		successorList: []NodeInfo{self.Core()},
		fingers:       fingers,
		status:        StatusInitializing,
		startedAt:     time.Now().UTC(),
		client:        client,
		tracker:       tracker,
		options:       opts,
		failures:      map[string]int{},
	}
	return n, nil
}

func NewNodeInfoFromURI(uri string) (NodeInfo, error) {
	normalized, err := NormalizeURI(uri)
	if err != nil {
		return NodeInfo{}, err
	}
	id, err := HashURI(normalized)
	if err != nil {
		return NodeInfo{}, err
	}
	return NodeInfo{NodeID: id, URI: normalized}, nil
}

func ValidateNodeInfo(node NodeInfo) error {
	if !ValidateID(node.NodeID) {
		return errors.New("node_id must be 40 lowercase hex characters")
	}
	normalized, err := NormalizeURI(node.URI)
	if err != nil {
		return err
	}
	if normalized != node.URI {
		return errors.New("uri must be normalized")
	}
	expected, err := HashURI(node.URI)
	if err != nil {
		return err
	}
	if expected != node.NodeID {
		return errors.New("node_id does not match sha1(uri)")
	}
	return nil
}

func (n *Node) Self() NodeInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.identityLocked()
}

func (n *Node) identityLocked() NodeInfo {
	info := n.self
	info.Status = n.status
	info.JoinedAt = n.joinedAt
	return info
}

func (n *Node) State() StateResponse {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return StateResponse{
		NodeID:            n.self.NodeID,
		URI:               n.self.URI,
		Status:            n.status,
		Predecessor:       cloneNodePtr(n.predecessor),
		Successor:         n.successor,
		SuccessorList:     cloneNodes(n.successorList),
		FingerTable:       cloneFingers(n.fingers),
		LastMaintenanceAt: cloneTimePtr(n.lastMaintenanceAt),
		NextFingerIndex:   n.nextFingerIndex,
	}
}

func (n *Node) FingerTable() FingerTableResponse {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return FingerTableResponse{NodeID: n.self.NodeID, FingerTable: cloneFingers(n.fingers), NextRepairIndex: n.nextFingerIndex}
}

func (n *Node) Ping() PingResponse {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return PingResponse{NodeID: n.self.NodeID, Status: n.status, Timestamp: time.Now().UTC()}
}

func (n *Node) Predecessor() PredecessorResponse {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return PredecessorResponse{Predecessor: cloneNodePtr(n.predecessor)}
}

func (n *Node) SuccessorList() SuccessorListResponse {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return SuccessorListResponse{SuccessorList: cloneNodes(n.successorList)}
}

func (n *Node) ActivateSingleNode() {
	n.mu.Lock()
	selfID := n.self.NodeID
	n.joinedAt = time.Now().UTC()
	n.status = StatusActive
	n.predecessor = nil
	n.successor = n.self.Core()
	n.successorList = []NodeInfo{n.self.Core()}
	for i := range n.fingers {
		n.fingers[i].Node = n.self.Core()
		n.fingers[i].Status = FingerOK
	}
	n.mu.Unlock()
	logging.Infof("node activated as single-node ring node_id=%s", selfID)
}

func (n *Node) JoinNetwork(manualSeeds []NodeInfo) error {
	var seeds []NodeInfo
	if n.tracker != nil {
		trackerSeeds, err := n.tracker.Seeds(n.options.TrackerSeedCount, []string{n.self.NodeID})
		if err == nil {
			seeds = append(seeds, trackerSeeds...)
			logging.Infof("tracker returned seeds count=%d", len(trackerSeeds))
		} else {
			logging.Warnf("tracker seed lookup failed: %v", err)
		}
	}
	logging.Infof("manual seeds configured count=%d", len(manualSeeds))
	seeds = append(seeds, manualSeeds...)
	seeds = dedupeNodes(seeds, n.self.NodeID)
	logging.Infof("joining network seeds=%d", len(seeds))

	n.mu.Lock()
	n.status = StatusJoining
	n.mu.Unlock()
	logging.Infof("node status changed status=%s", StatusJoining)

	if len(seeds) == 0 || n.client == nil {
		logging.Infof("no usable seeds found; activating single-node ring")
		n.ActivateSingleNode()
		n.registerTracker()
		return nil
	}

	self := n.Self().Core()
	selfWithCert := self
	selfWithCert.Certificate = n.options.NodeCertificate
	for _, seed := range seeds {
		if seed.NodeID == self.NodeID {
			logging.Debugf("ignoring self seed node_id=%s uri=%s", seed.NodeID, seed.URI)
			continue
		}
		if err := ValidateNodeInfo(seed); err != nil {
			logging.Warnf("ignoring invalid seed node_id=%s uri=%s error=%v", seed.NodeID, seed.URI, err)
			continue
		}
		logging.Debugf("attempting join via seed node_id=%s uri=%s", seed.NodeID, seed.URI)
		resp, err := n.client.Join(seed.URI, JoinRequest{Node: selfWithCert})
		if err != nil {
			logging.Warnf("join via seed failed node_id=%s uri=%s error=%v", seed.NodeID, seed.URI, err)
			continue
		}
		if err := ValidateNodeInfo(resp.Successor); err != nil {
			logging.Warnf("join via seed returned invalid successor seed_node_id=%s successor_node_id=%s successor_uri=%s error=%v", seed.NodeID, resp.Successor.NodeID, resp.Successor.URI, err)
			continue
		}
		n.mu.Lock()
		n.predecessor = nil
		n.successor = resp.Successor.Core()
		n.successorList = n.mergeSuccessorListLocked(resp.Successor.Core(), resp.SuccessorList)
		n.fingers[0].Node = resp.Successor.Core()
		n.fingers[0].Status = FingerOK
		n.joinedAt = time.Now().UTC()
		n.status = StatusActive
		n.mu.Unlock()
		logging.Infof("joined network via seed=%s successor=%s successor_list_size=%d", seed.NodeID, resp.Successor.NodeID, len(resp.SuccessorList))
		_, _ = n.client.Notify(resp.Successor.URI, NotifyRequest{Node: selfWithCert})
		n.registerTracker()
		return nil
	}

	logging.Warnf("all join attempts failed; activating single-node ring")
	n.ActivateSingleNode()
	n.registerTracker()
	return nil
}

func (n *Node) registerTracker() {
	if n.tracker != nil {
		self := n.Self().Core()
		self.Certificate = n.options.NodeCertificate
		if err := n.tracker.Register(self); err != nil {
			logging.Warnf("tracker registration failed node_id=%s error=%v", self.NodeID, err)
			return
		}
		logging.Infof("registered node with tracker node_id=%s", self.NodeID)
	}
}

func cloneNodePtr(node *NodeInfo) *NodeInfo {
	if node == nil {
		return nil
	}
	copy := *node
	return &copy
}

func cloneNodes(nodes []NodeInfo) []NodeInfo {
	out := make([]NodeInfo, len(nodes))
	copy(out, nodes)
	return out
}

func cloneFingers(fingers []FingerEntry) []FingerEntry {
	out := make([]FingerEntry, len(fingers))
	copy(out, fingers)
	return out
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	copy := *t
	return &copy
}

func dedupeNodes(nodes []NodeInfo, excludeID string) []NodeInfo {
	seen := map[string]bool{}
	out := make([]NodeInfo, 0, len(nodes))
	for _, node := range nodes {
		if node.NodeID == "" || node.NodeID == excludeID || seen[node.NodeID] {
			continue
		}
		seen[node.NodeID] = true
		out = append(out, node.Core())
	}
	return out
}
