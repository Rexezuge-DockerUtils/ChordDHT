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

	// v4.0 vnode options
	VNodeIndex              int           // 0 = anchor; 1+ = vnode
	AnchorID                string        // empty for anchors
	VNodeProofPtr           *VNodeProof   // current VNodeProof; nil for anchors
	MaxVNodes               int
	VNodeProofTTL           time.Duration
	VNodeProofRenewBefore   time.Duration
	ClockSkewTolerance      time.Duration
	VNodeGoroutineLimit     int
	VNodeMaintenanceJitter  time.Duration
	SiblingRouteMaxHops     int
	SuccessorListSiblingCap float64
	VNodeBootstrapPreferExt bool
	SharedNodeInfoCacheSize  int
	SharedNodeInfoCacheTTL   time.Duration
	SharedRTTCacheTTL        time.Duration
	SharedRouteCacheSize     int
	SharedRouteCacheTTL      time.Duration
	SharedProofVerifyCacheSize int
	TransferTimeout          time.Duration
	// Shared L0 resources; set on vnodes to share the anchor's caches.
	SharedRTTCache    *RTTCache
	SharedRoutingCache *RoutingCache
	// VNodeEntries allows the anchor to include its vnodes in tracker registration.
	VNodeEntries []VNodeEntry

	// v3.0 options
	Region                          string
	PredecessorListSize             int
	FixFingersBatchSizeActive       int
	FixFingersBatchSizeQuiet        int
	RoutingCacheEnabled             bool
	RoutingCacheSize                int
	RoutingCacheTTL                 time.Duration
	LatencyWeightID                 float64
	LatencyWeightRTT                float64
	LatencyWeightRegion             float64
	ParallelLookupEnabled           bool
	ParallelLookupCandidates        int
	TimeoutPingSameRegion           time.Duration
	TimeoutPingCrossRegion          time.Duration
	TimeoutFindSuccessorSame        time.Duration
	TimeoutFindSuccessorCross       time.Duration
	TimeoutFixFingersSame           time.Duration
	TimeoutFixFingersCross          time.Duration
	LatencyProbeIntervalActive      time.Duration
	LatencyProbeIntervalQuiet       time.Duration
	RTTEWMAAlpha                    float64
	RTTSampleExpiry                 time.Duration
	PiggybackEnabled                bool
	StabilizeDebounceThreshold      int
	TopologyChangeWindow            time.Duration
	StabilizeActiveInterval         time.Duration
	StabilizeQuietInterval          time.Duration
	FixFingersActiveInterval        time.Duration
	FixFingersQuietInterval         time.Duration
	CheckPredecessorActiveInterval  time.Duration
	CheckPredecessorQuietInterval   time.Duration
}

func DefaultOptions() Options {
	return Options{
		SuccessorListSize:   DefaultSuccessorListSize,
		MaintenanceInterval: DefaultMaintenanceInterval,
		MaxHops:             DefaultMaxHops,
		SuspiciousThreshold: DefaultSuspiciousThreshold,
		FailedThreshold:     DefaultFailedThreshold,
		TrackerSeedCount:    DefaultTrackerSeedCount,

		Region:                         "",
		PredecessorListSize:            DefaultPredecessorListSize,
		FixFingersBatchSizeActive:      DefaultFixFingersBatchSizeActive,
		FixFingersBatchSizeQuiet:       DefaultFixFingersBatchSizeQuiet,
		RoutingCacheEnabled:            true,
		RoutingCacheSize:               DefaultRoutingCacheSize,
		RoutingCacheTTL:                DefaultRoutingCacheTTL,
		LatencyWeightID:                0.6,
		LatencyWeightRTT:               0.3,
		LatencyWeightRegion:            0.1,
		ParallelLookupEnabled:          false,
		ParallelLookupCandidates:       3,
		TimeoutPingSameRegion:          DefaultTimeoutPingSameRegion,
		TimeoutPingCrossRegion:         DefaultTimeoutPingCrossRegion,
		TimeoutFindSuccessorSame:       DefaultTimeoutFindSuccessorSame,
		TimeoutFindSuccessorCross:      DefaultTimeoutFindSuccessorCross,
		TimeoutFixFingersSame:          DefaultTimeoutFixFingersSame,
		TimeoutFixFingersCross:         DefaultTimeoutFixFingersCross,
		LatencyProbeIntervalActive:     DefaultLatencyProbeActiveInterval,
		LatencyProbeIntervalQuiet:      DefaultLatencyProbeQuietInterval,
		RTTEWMAAlpha:                   DefaultRTTEWMAAlpha,
		RTTSampleExpiry:                DefaultRTTSampleExpiry,
		PiggybackEnabled:               true,
		StabilizeDebounceThreshold:     DefaultStabilizeDebounceThreshold,
		TopologyChangeWindow:           DefaultTopologyChangeWindow,
		StabilizeActiveInterval:        DefaultStabilizeActiveInterval,
		StabilizeQuietInterval:         DefaultStabilizeQuietInterval,
		FixFingersActiveInterval:       DefaultFixFingersActiveInterval,
		FixFingersQuietInterval:        DefaultFixFingersQuietInterval,
		CheckPredecessorActiveInterval: DefaultCheckPredecessorActiveInterval,
		CheckPredecessorQuietInterval:  DefaultCheckPredecessorQuietInterval,
	}
}

type Node struct {
	mu                sync.RWMutex
	self              NodeInfo
	predecessor       *NodeInfo
	predecessorList   []NodeInfo
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

	// v3.0 fields
	region                 string
	maintenanceMode        MaintenanceMode
	lastChangeTime         time.Time
	stabilizeDebounceCount int
	topologyChangeCh       chan struct{}
	rttCache               *RTTCache
	routingCache           *RoutingCache
}

func NewNode(uri string, opts Options, client PeerClient, tracker TrackerClient) (*Node, error) {
	var self NodeInfo
	var err error
	if opts.AnchorID != "" && opts.VNodeIndex > 0 {
		// Vnode: derive ID deterministically; share the anchor's physical URI.
		normalized, err := NormalizeURI(uri)
		if err != nil {
			return nil, err
		}
		self = NodeInfo{NodeID: DeriveVNodeID(opts.AnchorID, opts.VNodeIndex), URI: normalized}
	} else {
		self, err = NewNodeInfoFromURI(uri)
		if err != nil {
			return nil, err
		}
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

	// Use shared L0 caches when provided (vnode mode), otherwise create new ones.
	rttCache := opts.SharedRTTCache
	if rttCache == nil {
		rttCache = NewRTTCache(opts.RTTEWMAAlpha, opts.RTTSampleExpiry)
	}

	var rc *RoutingCache
	if opts.SharedRoutingCache != nil {
		rc = opts.SharedRoutingCache
	} else if opts.RoutingCacheEnabled {
		rc = NewRoutingCache(opts.RoutingCacheSize, opts.RoutingCacheTTL)
	}

	n := &Node{
		self:             self,
		successor:        self.Core(),
		successorList:    []NodeInfo{self.Core()},
		predecessorList:  make([]NodeInfo, 0, opts.PredecessorListSize),
		fingers:          fingers,
		status:           StatusInitializing,
		startedAt:        time.Now().UTC(),
		client:           client,
		tracker:          tracker,
		options:          opts,
		failures:         map[string]int{},
		region:           opts.Region,
		maintenanceMode:  QuietMaintenance,
		topologyChangeCh: make(chan struct{}, 1),
		rttCache:         rttCache,
		routingCache:     rc,
	}
	n.self.Region = opts.Region
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
	// Vnodes have a derived node_id (not sha1(uri)); skip the URI check.
	if node.AnchorID != "" {
		return nil
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
	if n.options.AnchorID != "" {
		info.AnchorID = n.options.AnchorID
		info.VNodeProof = n.options.VNodeProofPtr
	}
	return info
}

// IsVNode reports whether this node is a virtual node (not the anchor).
func (n *Node) IsVNode() bool {
	return n.options.VNodeIndex > 0 && n.options.AnchorID != ""
}

// VNodeInfo returns metadata about this vnode; only valid when IsVNode() is true.
func (n *Node) VNodeInfo() VNodeInfoResponse {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return VNodeInfoResponse{
		VNodeID:   n.self.NodeID,
		AnchorID:  n.options.AnchorID,
		Index:     n.options.VNodeIndex,
		Proof:     n.options.VNodeProofPtr,
		AnchorURI: n.self.URI,
	}
}

// SetVNodeEntries updates the list of vnode entries the anchor sends to the tracker.
func (n *Node) SetVNodeEntries(entries []VNodeEntry) {
	n.mu.Lock()
	n.options.VNodeEntries = entries
	n.mu.Unlock()
}

func (n *Node) State() StateResponse {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return StateResponse{
		NodeID:            n.self.NodeID,
		URI:               n.self.URI,
		Status:            n.status,
		Predecessor:       cloneNodePtr(n.predecessor),
		PredecessorList:   cloneNodes(n.predecessorList),
		Successor:         n.successor,
		SuccessorList:     cloneNodes(n.successorList),
		FingerTable:       cloneFingers(n.fingers),
		LastMaintenanceAt: cloneTimePtr(n.lastMaintenanceAt),
		NextFingerIndex:   n.nextFingerIndex,
		MaintenanceMode:   n.maintenanceMode,
		Region:            n.region,
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
	return PredecessorResponse{
		Predecessor:     cloneNodePtr(n.predecessor),
		PredecessorList: cloneNodes(n.predecessorList),
	}
}

func (n *Node) RTTData() RTTResponse {
	return RTTResponse{
		Samples: n.rttCache.Snapshot(),
		Region:  n.region,
	}
}

func (n *Node) NodeStatusInfo() NodeStatusResponse {
	n.mu.RLock()
	mode := n.maintenanceMode
	lastChange := n.lastChangeTime
	n.mu.RUnlock()

	var hits, misses int64
	var size int
	if n.routingCache != nil {
		hits, misses, size = n.routingCache.Stats()
	}
	return NodeStatusResponse{
		MaintenanceMode: mode,
		LastChangeTime:  lastChange,
		CacheHits:       hits,
		CacheMisses:     misses,
		CacheSize:       size,
		Region:          n.region,
	}
}

func (n *Node) emitTopologyChange() {
	select {
	case n.topologyChangeCh <- struct{}{}:
	default:
	}
	if n.routingCache != nil {
		n.routingCache.InvalidateAll()
	}
}

func (n *Node) switchMode(mode MaintenanceMode) {
	n.mu.Lock()
	n.maintenanceMode = mode
	n.mu.Unlock()
}

func (n *Node) buildPiggyback() *PiggybackData {
	if !n.options.PiggybackEnabled {
		return nil
	}
	n.mu.RLock()
	ids := make([]string, 0, len(n.successorList))
	for _, s := range n.successorList {
		ids = append(ids, s.NodeID)
	}
	n.mu.RUnlock()
	return &PiggybackData{
		SenderSuccessorList: ids,
		SenderRegion:        n.region,
		SenderRTTHints:      n.rttCache.Snapshot(),
	}
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
		_, _ = n.client.Notify(resp.Successor, NotifyRequest{Node: selfWithCert})
		n.registerTracker()
		go n.warmUpFingerTable()
		return nil
	}

	logging.Warnf("all join attempts failed; activating single-node ring")
	n.ActivateSingleNode()
	n.registerTracker()
	return nil
}

func (n *Node) registerTracker() {
	if n.tracker == nil {
		return
	}
	self := n.Self()
	self.Certificate = n.options.NodeCertificate
	// Attach vnode entries when registering as an anchor.
	n.mu.RLock()
	vnodes := n.options.VNodeEntries
	n.mu.RUnlock()
	if len(vnodes) > 0 {
		self.Vnodes = vnodes
	}
	region, err := n.tracker.Register(self)
	if err != nil {
		logging.Warnf("tracker registration failed node_id=%s error=%v", self.NodeID, err)
		return
	}
	if n.region == "" && region != "" {
		n.mu.Lock()
		n.region = region
		n.self.Region = region
		n.mu.Unlock()
		logging.Infof("region auto-detected from tracker node_id=%s region=%s", self.NodeID, region)
	}
	logging.Infof("registered node with tracker node_id=%s", self.NodeID)
}

func (n *Node) Region() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.region
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
