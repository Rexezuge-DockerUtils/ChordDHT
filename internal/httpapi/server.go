package httpapi

import (
	"encoding/json"
	"net/http"

	"chorddht/internal/chord"
)

type Server struct {
	node *chord.Node
}

func NewServer(node *chord.Node) *Server {
	return &Server{node: node}
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
	return mux
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
	resp, err := s.node.HandleJoin(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
