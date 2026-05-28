package auth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// VerifierConfig holds all configuration for the request verifier.
type VerifierConfig struct {
	CAPublicKey        ed25519.PublicKey
	TimestampTolerance time.Duration
	NonceCache         *NonceCache
	CertCache          *CertCache
	ToleranceSecs      int
	BootGracePeriodEnd time.Time // zero value = no grace period
}

// RequestVerifier is an HTTP middleware that enforces v2.0 request authentication.
type RequestVerifier struct {
	cfg VerifierConfig
	crl struct {
		mu  sync.RWMutex
		crl *CRL
	}
}

// NewRequestVerifier creates a verifier with the given config and optional initial CRL.
func NewRequestVerifier(cfg VerifierConfig, initialCRL *CRL) *RequestVerifier {
	v := &RequestVerifier{cfg: cfg}
	v.crl.crl = initialCRL
	return v
}

// SetCRL atomically replaces the in-memory CRL used for revocation checks.
func (v *RequestVerifier) SetCRL(crl *CRL) {
	v.crl.mu.Lock()
	defer v.crl.mu.Unlock()
	v.crl.crl = crl
}

// getCRL returns the current CRL (may be nil).
func (v *RequestVerifier) getCRL() *CRL {
	v.crl.mu.RLock()
	defer v.crl.mu.RUnlock()
	return v.crl.crl
}

// CacheIncomingCert stores a certificate that was already verified by the middleware
// (e.g., extracted from a join/notify request body).
func (v *RequestVerifier) CacheIncomingCert(cert *Certificate) {
	v.cfg.CertCache.StoreVerified(cert, time.Now())
}

// Middleware returns an http.Handler that enforces authentication on all paths except
// those listed in exemptPaths.
func (v *RequestVerifier) Middleware(next http.Handler, exemptPaths map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exemptPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// Boot grace period: accept all requests until the period expires.
		if !v.cfg.BootGracePeriodEnd.IsZero() && time.Now().Before(v.cfg.BootGracePeriodEnd) {
			next.ServeHTTP(w, r)
			return
		}

		now := time.Now()

		// Step 1: Extract required headers.
		nodeID := r.Header.Get("X-Chord-Node-ID")
		timestampStr := r.Header.Get("X-Chord-Timestamp")
		nonce := r.Header.Get("X-Chord-Nonce")
		sigStr := r.Header.Get("X-Chord-Signature")
		if nodeID == "" || timestampStr == "" || nonce == "" || sigStr == "" {
			writeAuthError(w, http.StatusUnauthorized, "MISSING_AUTH_HEADERS", "missing required authentication headers")
			return
		}

		// Step 2: Time window check.
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "TIMESTAMP_OUT_OF_WINDOW", "invalid timestamp")
			return
		}
		diff := now.Unix() - timestamp
		if diff < 0 {
			diff = -diff
		}
		if time.Duration(diff)*time.Second > v.cfg.TimestampTolerance {
			writeAuthError(w, http.StatusUnauthorized, "TIMESTAMP_OUT_OF_WINDOW", "request timestamp outside acceptable window")
			return
		}

		// Step 3: Nonce deduplication.
		if !v.cfg.NonceCache.Add(nonce, now) {
			writeAuthError(w, http.StatusUnauthorized, "NONCE_REUSED", "nonce has already been used")
			return
		}

		// Step 4: Certificate acquisition.
		var cert *Certificate
		if certHeader := r.Header.Get("X-Chord-Certificate"); certHeader != "" {
			parsed, err := ParseCertificateBase64URL(certHeader)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "INVALID_CERTIFICATE", fmt.Sprintf("cert header parse failed: %v", err))
				return
			}
			if err := VerifyCertificate(parsed, v.cfg.CAPublicKey, now, v.cfg.ToleranceSecs); err != nil {
				writeAuthError(w, http.StatusUnauthorized, "INVALID_CERTIFICATE", err.Error())
				return
			}
			// URI consistency: X-Chord-Node-ID must match cert.NodeID
			if parsed.NodeID != nodeID {
				writeAuthError(w, http.StatusUnauthorized, "INVALID_CERTIFICATE", "cert node_id does not match X-Chord-Node-ID")
				return
			}
			cert = parsed
			// Cache the verified cert (may be updated to newer if TOFU allows)
			v.cfg.CertCache.StoreVerified(parsed, now)
		} else {
			cached, ok := v.cfg.CertCache.Get(nodeID, now)
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "CERTIFICATE_REQUIRED", "no certificate cached for this node; resend with X-Chord-Certificate header")
				return
			}
			cert = cached
		}

		// Step 5: Revocation check.
		if crl := v.getCRL(); crl.IsRevoked(cert.NodeID) {
			writeAuthError(w, http.StatusUnauthorized, "CERTIFICATE_REVOKED", "node certificate has been revoked")
			return
		}

		// Step 6: Buffer body for signature verification.
		const maxBodySize = 1 << 20 // 1 MB
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "INVALID_SIGNATURE", "failed to read request body")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// Step 7: Verify request signature.
		bodySum := sha256.Sum256(bodyBytes)
		bodyHash := hex.EncodeToString(bodySum[:])
		path := r.URL.RequestURI()
		msg := []byte(fmt.Sprintf(
			"chord-request-v1\n%s\n%s\n%s\n%s\n%s\n%s",
			r.Method, path, nodeID, timestampStr, nonce, bodyHash,
		))

		sigBytes, err := base64.RawURLEncoding.DecodeString(sigStr)
		if err != nil || len(sigBytes) != 64 {
			writeAuthError(w, http.StatusUnauthorized, "INVALID_SIGNATURE", "malformed signature")
			return
		}
		pubKey, err := cert.NodePublicKey()
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "INVALID_CERTIFICATE", "cannot decode node public key")
			return
		}
		if !ed25519.Verify(pubKey, msg, sigBytes) {
			writeAuthError(w, http.StatusUnauthorized, "INVALID_SIGNATURE", "request signature verification failed")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeAuthError writes a JSON error response in the same shape as httpapi/errors.go.
func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Detail  map[string]any `json:"detail"`
		} `json:"error"`
	}{}
	resp.Error.Code = code
	resp.Error.Message = message
	resp.Error.Detail = map[string]any{}
	_ = json.NewEncoder(w).Encode(resp)
}
