package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"chorddht/internal/chord"
)

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	var apiErr *chord.APIError
	if errors.As(err, &apiErr) {
		writeJSON(w, apiErr.StatusCode, errorResponse{Error: errorBody{Code: apiErr.Code, Message: apiErr.Message, Detail: apiErr.Detail}})
		return
	}
	writeJSON(w, http.StatusInternalServerError, errorResponse{Error: errorBody{Code: chord.ErrUpstream, Message: err.Error(), Detail: map[string]any{}}})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: errorBody{Code: "METHOD_NOT_ALLOWED", Message: "method not allowed", Detail: map[string]any{}}})
}
