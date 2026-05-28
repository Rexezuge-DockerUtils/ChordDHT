package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// RequestSigner signs outbound Chord HTTP requests with Ed25519.
type RequestSigner struct {
	nodeID  string
	privKey ed25519.PrivateKey
	cert    *Certificate
}

// NewRequestSigner creates a signer for outbound requests.
func NewRequestSigner(nodeID string, privKey ed25519.PrivateKey, cert *Certificate) *RequestSigner {
	return &RequestSigner{nodeID: nodeID, privKey: privKey, cert: cert}
}

// Sign adds the four required authentication headers to req.
// bodyBytes is the serialised request body; pass nil for requests with no body.
func (s *RequestSigner) Sign(req *http.Request, bodyBytes []byte) error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	var bodyForHash []byte
	if bodyBytes != nil {
		bodyForHash = bodyBytes
	}
	sum := sha256.Sum256(bodyForHash)
	bodyHash := hex.EncodeToString(sum[:])

	path := req.URL.RequestURI()
	msg := []byte(fmt.Sprintf(
		"chord-request-v1\n%s\n%s\n%s\n%s\n%s\n%s",
		req.Method, path, s.nodeID, timestamp, nonce, bodyHash,
	))

	sig := ed25519.Sign(s.privKey, msg)

	req.Header.Set("X-Chord-Node-ID", s.nodeID)
	req.Header.Set("X-Chord-Timestamp", timestamp)
	req.Header.Set("X-Chord-Nonce", nonce)
	req.Header.Set("X-Chord-Signature", base64.RawURLEncoding.EncodeToString(sig))
	return nil
}

// AddCertHeader sets X-Chord-Certificate on req with the signer's certificate.
func (s *RequestSigner) AddCertHeader(req *http.Request) error {
	if s.cert == nil {
		return nil
	}
	encoded, err := s.cert.MarshalBase64URL()
	if err != nil {
		return err
	}
	req.Header.Set("X-Chord-Certificate", encoded)
	return nil
}

// NodeID returns the signer's node_id.
func (s *RequestSigner) NodeID() string {
	return s.nodeID
}

// CertExpiresAt returns the certificate expiry as a Unix timestamp pointer, or nil if no cert.
func (s *RequestSigner) CertExpiresAt() *int64 {
	if s.cert == nil {
		return nil
	}
	v := s.cert.ExpiresAt
	return &v
}
