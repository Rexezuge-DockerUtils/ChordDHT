package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"chorddht/internal/auth"
	"chorddht/internal/chord"
	"chorddht/internal/logging"
)

type Server struct {
	node     *chord.Node
	verifier *auth.RequestVerifier // nil when auth is disabled
}

func NewServer(node *chord.Node, verifier *auth.RequestVerifier) *Server {
	return &Server{node: node, verifier: verifier}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/chord/identity", s.identity)
	mux.HandleFunc("/chord/state", s.state)
	mux.HandleFunc("/chord/ping", s.ping)
	mux.HandleFunc("/chord/find_successor", s.findSuccessor)
	mux.HandleFunc("/chord/predecessor", s.predecessor)
	mux.HandleFunc("/chord/notify", s.notify)
	mux.HandleFunc("/chord/successor_list", s.successorList)
	mux.HandleFunc("/chord/join", s.join)
	mux.HandleFunc("/chord/leave", s.leave)
	mux.HandleFunc("/chord/finger_table", s.fingerTable)

	var handler http.Handler = mux
	if s.verifier != nil {
		exempt := map[string]bool{
			"/chord/ping":     true,
			"/chord/identity": true,
		}
		handler = s.verifier.Middleware(mux, exempt)
	}
	return logRequests(handler)
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

func (s *Server) identity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.node.Self())
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.node.State())
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.node.Ping())
}

func (s *Server) findSuccessor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req chord.FindSuccessorRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	resp, err := s.node.HandleFindSuccessor(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) predecessor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.node.Predecessor())
}

func (s *Server) notify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req chord.NotifyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	s.cacheNodeCert(req.Node.Certificate)
	resp, err := s.node.HandleNotify(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) successorList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.node.SuccessorList())
}

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req chord.JoinRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	s.cacheNodeCert(req.Node.Certificate)
	resp, err := s.node.HandleJoin(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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

func (s *Server) leave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req chord.LeaveRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	resp, err := s.node.HandleLeave(req)
	if err != nil {
		writeError(w, err)
		return
	}
	go s.node.Stabilize()
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) fingerTable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.node.FingerTable())
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
