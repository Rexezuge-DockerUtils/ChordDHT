package chord

import (
	"encoding/json"
	"time"
)

const (
	DefaultM                    = 160
	DefaultSuccessorListSize    = 5
	DefaultMaintenanceInterval  = 60 * time.Second
	DefaultHTTPTimeout          = 5 * time.Second
	DefaultMaxHops              = 161
	DefaultSuspiciousThreshold  = 1
	DefaultFailedThreshold      = 3
	DefaultTrackerSeedCount     = 5
	DefaultTrackerHeartbeat     = 60 * time.Second
	DefaultTrackerStaleInterval = 180 * time.Second

	DefaultPredecessorListSize = 2

	DefaultFixFingersBatchSizeActive = 8
	DefaultFixFingersBatchSizeQuiet  = 4

	DefaultRoutingCacheSize = 1000
	DefaultRoutingCacheTTL  = 30 * time.Second

	DefaultRTTEWMAAlpha    = 0.3
	DefaultRTTSampleExpiry = 300 * time.Second

	DefaultTopologyChangeWindow = 120 * time.Second

	DefaultStabilizeActiveInterval           = 15 * time.Second
	DefaultStabilizeQuietInterval            = 60 * time.Second
	DefaultFixFingersActiveInterval          = 10 * time.Second
	DefaultFixFingersQuietInterval           = 30 * time.Second
	DefaultCheckPredecessorActiveInterval    = 10 * time.Second
	DefaultCheckPredecessorQuietInterval     = 30 * time.Second
	DefaultLatencyProbeActiveInterval        = 30 * time.Second
	DefaultLatencyProbeQuietInterval         = 120 * time.Second
	DefaultStabilizeDebounceThreshold        = 3

	DefaultTimeoutPingSameRegion        = 2 * time.Second
	DefaultTimeoutPingCrossRegion       = 5 * time.Second
	DefaultTimeoutFindSuccessorSame     = 5 * time.Second
	DefaultTimeoutFindSuccessorCross    = 15 * time.Second
	DefaultTimeoutFixFingersSame        = 5 * time.Second
	DefaultTimeoutFixFingersCross       = 30 * time.Second
)

type Status string

const (
	StatusInitializing Status = "INITIALIZING"
	StatusJoining      Status = "JOINING"
	StatusActive       Status = "ACTIVE"
	StatusLeaving      Status = "LEAVING"
	StatusIsolated     Status = "ISOLATED"
)

type FingerStatus string

const (
	FingerOK         FingerStatus = "ok"
	FingerSuspicious FingerStatus = "suspicious"
	FingerUnknown    FingerStatus = "unknown"
)

type NodeInfo struct {
	NodeID       string          `json:"node_id"`
	URI          string          `json:"uri"`
	Status       Status          `json:"status,omitempty"`
	JoinedAt     time.Time       `json:"joined_at,omitempty"`
	LastSeen     time.Time       `json:"last_seen,omitempty"`
	Certificate  json.RawMessage `json:"certificate,omitempty"`
	Region       string          `json:"region,omitempty"`
	Version      string          `json:"version,omitempty"`
	Capabilities []string        `json:"capabilities,omitempty"`
}

func (n NodeInfo) Core() NodeInfo {
	return NodeInfo{NodeID: n.NodeID, URI: n.URI, Region: n.Region}
}

type FingerEntry struct {
	Index        int          `json:"index"`
	Start        string       `json:"start"`
	Node         NodeInfo     `json:"node"`
	Status       FingerStatus `json:"status"`
	Valid        bool         `json:"valid"`
	LastVerified time.Time    `json:"last_verified,omitempty"`
}

type MaintenanceMode string

const (
	ActiveMaintenance MaintenanceMode = "ACTIVE_MAINTENANCE"
	QuietMaintenance  MaintenanceMode = "QUIET_MAINTENANCE"
)

type PiggybackData struct {
	SenderSuccessorList []string         `json:"sender_successor_list,omitempty"`
	SenderRegion        string           `json:"sender_region,omitempty"`
	SenderRTTHints      map[string]int64 `json:"sender_rtt_hints,omitempty"`
}

type FindSuccessorRequest struct {
	ID              string   `json:"id"`
	HopCount        int      `json:"hop_count"`
	MaxHops         int      `json:"max_hops"`
	VisitedNodes    []string `json:"visited_nodes"`
	RequesterRegion string   `json:"requester_region,omitempty"`
	PreferRegion    string   `json:"prefer_region,omitempty"`
}

type FindSuccessorResponse struct {
	Found     bool           `json:"found"`
	Successor *NodeInfo      `json:"successor,omitempty"`
	NextHop   *NodeInfo      `json:"next_hop,omitempty"`
	HopCount  int            `json:"hop_count"`
	Cached    bool           `json:"cached,omitempty"`
	Piggyback *PiggybackData `json:"piggyback,omitempty"`
}

type JoinRequest struct {
	Node NodeInfo `json:"node"`
}

type JoinResponse struct {
	Successor     NodeInfo   `json:"successor"`
	SuccessorList []NodeInfo `json:"successor_list"`
}

type NotifyRequest struct {
	Node NodeInfo `json:"node"`
}

type NotifyResponse struct {
	Accepted    bool      `json:"accepted"`
	Predecessor *NodeInfo `json:"predecessor"`
}

type PredecessorResponse struct {
	Predecessor     *NodeInfo  `json:"predecessor"`
	PredecessorList []NodeInfo `json:"predecessor_list,omitempty"`
}

type SuccessorListResponse struct {
	SuccessorList []NodeInfo `json:"successor_list"`
}

type PingResponse struct {
	NodeID    string    `json:"node_id"`
	Status    Status    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

type LeaveRequest struct {
	Role           string    `json:"role"`
	NewPredecessor *NodeInfo `json:"new_predecessor,omitempty"`
	NewSuccessor   *NodeInfo `json:"new_successor,omitempty"`
}

type LeaveResponse struct {
	Acknowledged bool `json:"acknowledged"`
}

type StateResponse struct {
	NodeID            string          `json:"node_id"`
	URI               string          `json:"uri"`
	Status            Status          `json:"status"`
	Predecessor       *NodeInfo       `json:"predecessor"`
	PredecessorList   []NodeInfo      `json:"predecessor_list,omitempty"`
	Successor         NodeInfo        `json:"successor"`
	SuccessorList     []NodeInfo      `json:"successor_list"`
	FingerTable       []FingerEntry   `json:"finger_table"`
	LastMaintenanceAt *time.Time      `json:"last_maintenance_at"`
	NextFingerIndex   int             `json:"next_finger_index"`
	MaintenanceMode   MaintenanceMode `json:"maintenance_mode,omitempty"`
	Region            string          `json:"region,omitempty"`
}

type FingerTableResponse struct {
	NodeID          string        `json:"node_id"`
	FingerTable     []FingerEntry `json:"finger_table"`
	NextRepairIndex int           `json:"next_repair_index"`
}

type TrackerHeartbeat struct {
	Status                Status  `json:"status"`
	SuccessorID           *string `json:"successor_id"`
	PredecessorID         *string `json:"predecessor_id"`
	SuccessorListSize     int     `json:"successor_list_size"`
	SuccessorListCapacity int     `json:"successor_list_capacity"`
	FingerTableCoverage   float64 `json:"finger_table_coverage"`
	UptimeSeconds         int64   `json:"uptime_seconds"`
	MaintenanceCycles     uint64  `json:"maintenance_cycles"`
	CertExpiresAt         *int64  `json:"cert_expires_at,omitempty"`
	Region                string  `json:"region,omitempty"`
}

type RTTResponse struct {
	Samples map[string]int64 `json:"samples"`
	Region  string           `json:"region"`
}

type NodeStatusResponse struct {
	MaintenanceMode MaintenanceMode `json:"maintenance_mode"`
	LastChangeTime  time.Time       `json:"last_change_time"`
	CacheHits       int64           `json:"cache_hits"`
	CacheMisses     int64           `json:"cache_misses"`
	CacheSize       int             `json:"cache_size"`
	Region          string          `json:"region"`
}

type PeerClient interface {
	Ping(uri string) error
	PingWithLatency(uri string) (int64, error)
	FindSuccessor(uri string, req FindSuccessorRequest) (FindSuccessorResponse, error)
	Join(uri string, req JoinRequest) (JoinResponse, error)
	Notify(uri string, req NotifyRequest) (NotifyResponse, error)
	Predecessor(uri string) (PredecessorResponse, error)
	SuccessorList(uri string) (SuccessorListResponse, error)
	Leave(uri string, req LeaveRequest) error
	RTT(uri string) (RTTResponse, error)
}

type TrackerClient interface {
	Seeds(count int, exclude []string) ([]NodeInfo, error)
	Register(node NodeInfo) error
	Deregister(nodeID string) error
	Heartbeat(nodeID string, heartbeat TrackerHeartbeat) error
	FetchCRL() ([]byte, error)
}
