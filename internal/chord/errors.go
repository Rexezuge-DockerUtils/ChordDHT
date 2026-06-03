package chord

import "fmt"

const (
	ErrInvalidRequest  = "INVALID_REQUEST"
	ErrNodeNotFound    = "NODE_NOT_FOUND"
	ErrIDCollision     = "ID_COLLISION"
	ErrNodeIsolated    = "NODE_ISOLATED"
	ErrMaxHopsExceeded = "MAX_HOPS_EXCEEDED"
	ErrNodeLeaving     = "NODE_LEAVING"
	ErrUpstreamTimeout = "UPSTREAM_TIMEOUT"
	ErrLoopDetected    = "LOOP_DETECTED"
	ErrUpstream        = "UPSTREAM_ERROR"

	ErrInvalidVNodeProof = "INVALID_VNODE_PROOF"
	ErrProofExpired      = "PROOF_EXPIRED"

	ErrMissingAuthHeaders   = "MISSING_AUTH_HEADERS"
	ErrTimestampOutOfWindow = "TIMESTAMP_OUT_OF_WINDOW"
	ErrNonceReused          = "NONCE_REUSED"
	ErrCertificateRequired  = "CERTIFICATE_REQUIRED"
	ErrInvalidCertificate   = "INVALID_CERTIFICATE"
	ErrCertificateRevoked   = "CERTIFICATE_REVOKED"
	ErrInvalidSignature     = "INVALID_SIGNATURE"
)

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Detail     map[string]any
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewAPIError(status int, code, message string) *APIError {
	return &APIError{StatusCode: status, Code: code, Message: message, Detail: map[string]any{}}
}
