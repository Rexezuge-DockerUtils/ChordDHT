package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"chorddht/internal/auth"
	"chorddht/internal/chord"
	"chorddht/internal/logging"
)

// NodePool holds the anchor node and all its vnodes for a single physical host.
type NodePool struct {
	Anchor *chord.Node
	VNodes []*chord.Node
}

// NewNodePool creates a NodePool. Additional vnodes may be passed after the anchor.
func NewNodePool(anchor *chord.Node, vnodes ...*chord.Node) *NodePool {
	return &NodePool{Anchor: anchor, VNodes: vnodes}
}

// byID returns the node with the given node_id, or nil if not found.
func (p *NodePool) byID(nodeID string) *chord.Node {
	if p.Anchor.Self().NodeID == nodeID {
		return p.Anchor
	}
	for _, vn := range p.VNodes {
		if vn.Self().NodeID == nodeID {
			return vn
		}
	}
	return nil
}

type Server struct {
	pool     *NodePool
	verifier *auth.RequestVerifier
}

func NewServer(pool *NodePool, verifier *auth.RequestVerifier) *Server {
	return &Server{pool: pool, verifier: verifier}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	anchor := s.pool.Anchor

	// Legacy /chord/* routes → anchor (backwards compat with v1–v3 nodes).
	s.registerNodeRoutes(mux, "/chord/", anchor)

	// Per-node routes for anchor and each vnode under /chord/node/{id}/.
	allNodes := append([]*chord.Node{anchor}, s.pool.VNodes...)
	for _, node := range allNodes {
		node := node
		base := "/chord/node/" + node.Self().NodeID + "/"
		s.registerNodeRoutes(mux, base, node)
		if node.IsVNode() {
			mux.HandleFunc(base+"vnode_info", s.makeVNodeInfoHandler(node))
		} else {
			mux.HandleFunc(base+"list_vnodes", s.listVnodes)
		}
	}

	// Catch-all for /chord/node/ → 404 when node_id not found.
	mux.HandleFunc("/chord/node/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, chord.NewAPIError(http.StatusNotFound, chord.ErrNodeNotFound, "node not found"))
	})

	var handler http.Handler = mux
	if s.verifier != nil {
		handler = s.verifier.Middleware(mux, s.exemptPaths)
	}
	return logRequests(handler)
}

// registerNodeRoutes registers all standard Chord API routes under the given base prefix.
func (s *Server) registerNodeRoutes(mux *http.ServeMux, base string, node *chord.Node) {
	mux.HandleFunc(base+"identity", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, node.Self())
	})
	mux.HandleFunc(base+"state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, node.State())
	})
	mux.HandleFunc(base+"ping", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, node.Ping())
	})
	mux.HandleFunc(base+"find_successor", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req chord.FindSuccessorRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		resp, err := node.HandleFindSuccessor(req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc(base+"predecessor", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, node.Predecessor())
	})
	mux.HandleFunc(base+"notify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !node.RectifyEndpointAliasEnabled() {
			writeError(w, chord.NewAPIError(http.StatusNotFound, chord.ErrNodeNotFound, "notify alias is disabled"))
			return
		}
		var req chord.NotifyRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		s.cacheNodeCert(req.Node.Certificate)
		resp, err := node.HandleRectify(req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc(base+"rectify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req chord.RectifyRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		s.cacheNodeCert(req.Node.Certificate)
		resp, err := node.HandleRectify(req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc(base+"successor_list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, node.SuccessorList())
	})
	mux.HandleFunc(base+"join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req chord.JoinRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		s.cacheNodeCert(req.Node.Certificate)
		resp, err := node.HandleJoin(req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc(base+"leave", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req chord.LeaveRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		resp, err := node.HandleLeave(req)
		if err != nil {
			writeError(w, err)
			return
		}
		go node.Stabilize()
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc(base+"finger_table", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, node.FingerTable())
	})
	mux.HandleFunc(base+"rtt", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, node.RTTData())
	})
	mux.HandleFunc(base+"status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, node.NodeStatusInfo())
	})
	mux.HandleFunc(base+"invariant", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, node.InvariantReport())
	})
	mux.HandleFunc(base+"transfer_keys", makeTransferKeysHandler(node))
	mux.HandleFunc(base+"transfer_ack", makeTransferAckHandler(node))
}

func (s *Server) makeVNodeInfoHandler(node *chord.Node) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, node.VNodeInfo())
	}
}

func (s *Server) listVnodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	anchor := s.pool.Anchor
	entries := make([]chord.VNodeEntry, 0, len(s.pool.VNodes))
	for _, vn := range s.pool.VNodes {
		self := vn.Self()
		entries = append(entries, chord.VNodeEntry{
			VNodeID: self.NodeID,
			Index:   vn.VNodeInfo().Index,
			Status:  self.Status,
		})
	}
	writeJSON(w, http.StatusOK, chord.ListVNodesResponse{
		AnchorID: anchor.Self().NodeID,
		Vnodes:   entries,
	})
}

func makeTransferKeysHandler(_ *chord.Node) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req chord.TransferKeysRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		// Stub: no data-storage layer in the base system.
		writeJSON(w, http.StatusOK, chord.TransferKeysResponse{
			Received: 0,
			BatchSeq: req.BatchSeq,
			AckToken: "stub",
		})
	}
}

func makeTransferAckHandler(_ *chord.Node) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req chord.TransferAckRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		// Stub: no data-storage layer in the base system.
		writeJSON(w, http.StatusOK, chord.TransferAckResponse{Status: "ok"})
	}
}

// exemptPaths reports whether a path is exempt from authentication.
func (s *Server) exemptPaths(path string) bool {
	switch path {
	case "/chord/ping", "/chord/identity", "/chord/rtt", "/chord/status":
		return true
	}
	if strings.HasPrefix(path, "/chord/node/") {
		rest := strings.TrimPrefix(path, "/chord/node/")
		// /chord/node/{id}/{op} — find the op part
		if slash := strings.Index(rest, "/"); slash >= 0 {
			op := rest[slash+1:]
			switch op {
			case "ping", "identity", "rtt", "status":
				return true
			}
		}
	}
	return false
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		duration := time.Since(start)
		if recorder.status >= http.StatusBadRequest {
			logging.Warnf("http request method=%s path=%s status=%d remote=%s duration=%s", r.Method, r.URL.Path, recorder.status, r.RemoteAddr, duration)
			return
		}
		logging.Debugf("http request method=%s path=%s status=%d remote=%s duration=%s", r.Method, r.URL.Path, recorder.status, r.RemoteAddr, duration)
	})
}

// cacheNodeCert stores a certificate from an incoming request body into the cert cache.
func (s *Server) cacheNodeCert(raw json.RawMessage) {
	if s.verifier == nil || len(raw) == 0 {
		return
	}
	cert, err := auth.ParseCertificate(raw)
	if err != nil {
		return
	}
	s.verifier.CacheIncomingCert(cert)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		writeError(w, chord.NewAPIError(http.StatusBadRequest, chord.ErrInvalidRequest, err.Error()))
		return false
	}
	return true
}
