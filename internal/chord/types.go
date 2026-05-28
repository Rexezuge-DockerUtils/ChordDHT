package chord

import "time"

const (
	DefaultM                    = 160
	DefaultSuccessorListSize    = 3
	DefaultMaintenanceInterval  = 60 * time.Second
	DefaultHTTPTimeout          = 5 * time.Second
	DefaultMaxHops              = 161
	DefaultSuspiciousThreshold  = 1
	DefaultFailedThreshold      = 3
	DefaultTrackerSeedCount     = 5
	DefaultTrackerHeartbeat     = 60 * time.Second
	DefaultTrackerStaleInterval = 180 * time.Second
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
	NodeID   string    `json:"node_id"`
	URI      string    `json:"uri"`
	Status   Status    `json:"status,omitempty"`
	JoinedAt time.Time `json:"joined_at,omitempty"`
	LastSeen time.Time `json:"last_seen,omitempty"`
}

func (n NodeInfo) Core() NodeInfo {
	return NodeInfo{NodeID: n.NodeID, URI: n.URI}
}

type FingerEntry struct {
	Index  int          `json:"index"`
	Start  string       `json:"start"`
	Node   NodeInfo     `json:"node"`
	Status FingerStatus `json:"status"`
}

type FindSuccessorRequest struct {
	ID           string   `json:"id"`
	HopCount     int      `json:"hop_count"`
	MaxHops      int      `json:"max_hops"`
	VisitedNodes []string `json:"visited_nodes"`
}

type FindSuccessorResponse struct {
	Found     bool      `json:"found"`
	Successor *NodeInfo `json:"successor,omitempty"`
	NextHop   *NodeInfo `json:"next_hop,omitempty"`
	HopCount  int       `json:"hop_count"`
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
	Predecessor *NodeInfo `json:"predecessor"`
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
	NodeID            string        `json:"node_id"`
	URI               string        `json:"uri"`
	Status            Status        `json:"status"`
	Predecessor       *NodeInfo     `json:"predecessor"`
	Successor         NodeInfo      `json:"successor"`
	SuccessorList     []NodeInfo    `json:"successor_list"`
	FingerTable       []FingerEntry `json:"finger_table"`
	LastMaintenanceAt *time.Time    `json:"last_maintenance_at"`
	NextFingerIndex   int           `json:"next_finger_index"`
}

type FingerTableResponse struct {
	NodeID          string        `json:"node_id"`
	FingerTable     []FingerEntry `json:"finger_table"`
	NextRepairIndex int           `json:"next_repair_index"`
}

type TrackerHeartbeat struct {
	Status              Status  `json:"status"`
	SuccessorID         *string `json:"successor_id"`
	PredecessorID       *string `json:"predecessor_id"`
	SuccessorListSize   int     `json:"successor_list_size"`
	FingerTableCoverage float64 `json:"finger_table_coverage"`
	UptimeSeconds       int64   `json:"uptime_seconds"`
	MaintenanceCycles   uint64  `json:"maintenance_cycles"`
}

type PeerClient interface {
	Ping(uri string) error
	FindSuccessor(uri string, req FindSuccessorRequest) (FindSuccessorResponse, error)
	Join(uri string, req JoinRequest) (JoinResponse, error)
	Notify(uri string, req NotifyRequest) (NotifyResponse, error)
	Predecessor(uri string) (PredecessorResponse, error)
	SuccessorList(uri string) (SuccessorListResponse, error)
	Leave(uri string, req LeaveRequest) error
}

type TrackerClient interface {
	Seeds(count int, exclude []string) ([]NodeInfo, error)
	Register(node NodeInfo) error
	Deregister(nodeID string) error
	Heartbeat(nodeID string, heartbeat TrackerHeartbeat) error
}
