package auth

import (
	"crypto/ed25519"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// Certificate is the custom lightweight JSON certificate format defined in v2.0 spec §2.2.
type Certificate struct {
	Version   int    `json:"version"`
	NodeID    string `json:"node_id"`
	URI       string `json:"uri"`
	PublicKey string `json:"public_key"` // base64url raw 32-byte Ed25519 public key
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
	Signature string `json:"signature"` // base64url 64-byte CA Ed25519 signature
}

// ParseCertificate decodes raw JSON bytes into a Certificate.
func ParseCertificate(data []byte) (*Certificate, error) {
	var c Certificate
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("cert parse: %w", err)
	}
	return &c, nil
}

// certSignedMessage builds the canonical signing input per spec §2.3.
// Format: "chord-cert-v1\nnode_id={}\nuri={}\npublic_key={}\nissued_at={}\nexpires_at={}"
func certSignedMessage(c *Certificate) []byte {
	return []byte(fmt.Sprintf(
		"chord-cert-v1\nnode_id=%s\nuri=%s\npublic_key=%s\nissued_at=%d\nexpires_at=%d",
		c.NodeID, c.URI, c.PublicKey, c.IssuedAt, c.ExpiresAt,
	))
}

// VerifyCertificate checks format, CA signature, validity period, and URI/node_id consistency.
// caPublicKey is the raw 32-byte Ed25519 public key.
func VerifyCertificate(c *Certificate, caPublicKey ed25519.PublicKey, now time.Time, toleranceSecs int) error {
	if c.Version != 1 {
		return errors.New("unsupported certificate version")
	}
	if len(c.NodeID) != 40 {
		return errors.New("cert node_id must be 40 hex characters")
	}
	pubKeyBytes, err := base64.RawURLEncoding.DecodeString(c.PublicKey)
	if err != nil || len(pubKeyBytes) != 32 {
		return errors.New("cert public_key must be base64url-encoded 32-byte Ed25519 key")
	}
	if c.IssuedAt >= c.ExpiresAt {
		return errors.New("cert issued_at must be before expires_at")
	}

	// URI consistency: node_id must equal SHA1(normalizedURI)
	normalized, err := normalizeURI(c.URI)
	if err != nil {
		return fmt.Errorf("cert uri invalid: %w", err)
	}
	sum := sha1.Sum([]byte(normalized))
	expectedID := hex.EncodeToString(sum[:])
	if c.NodeID != expectedID {
		return fmt.Errorf("cert node_id %s does not match sha1(uri) %s", c.NodeID, expectedID)
	}

	// CA signature verification
	sigBytes, err := base64.RawURLEncoding.DecodeString(c.Signature)
	if err != nil || len(sigBytes) != 64 {
		return errors.New("cert signature must be base64url-encoded 64-byte Ed25519 signature")
	}
	msg := certSignedMessage(c)
	if !ed25519.Verify(caPublicKey, msg, sigBytes) {
		return errors.New("cert CA signature verification failed")
	}

	// Validity period with tolerance
	tol := time.Duration(toleranceSecs) * time.Second
	if now.Before(time.Unix(c.IssuedAt, 0).Add(-tol)) {
		return errors.New("cert not yet valid")
	}
	if now.After(time.Unix(c.ExpiresAt, 0).Add(tol)) {
		return errors.New("cert has expired")
	}

	return nil
}

// NodePublicKey decodes the node's Ed25519 public key from the certificate.
func (c *Certificate) NodePublicKey() (ed25519.PublicKey, error) {
	b, err := base64.RawURLEncoding.DecodeString(c.PublicKey)
	if err != nil || len(b) != 32 {
		return nil, errors.New("invalid public key in certificate")
	}
	return ed25519.PublicKey(b), nil
}

// IsExpired reports whether the certificate is expired at the given time (with no tolerance).
func (c *Certificate) IsExpired(now time.Time) bool {
	return now.After(time.Unix(c.ExpiresAt, 0))
}

// IsExpiringSoon reports whether the cert expires within warnDays.
func (c *Certificate) IsExpiringSoon(now time.Time, warnDays int) bool {
	deadline := now.Add(time.Duration(warnDays) * 24 * time.Hour)
	return deadline.After(time.Unix(c.ExpiresAt, 0))
}

// MarshalBase64URL encodes the certificate as base64url(JSON).
func (c *Certificate) MarshalBase64URL() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// ParseCertificateBase64URL decodes a base64url-encoded certificate header value.
func ParseCertificateBase64URL(s string) (*Certificate, error) {
	if len(s) > 4096 {
		return nil, errors.New("X-Chord-Certificate header too large")
	}
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("X-Chord-Certificate base64url decode: %w", err)
	}
	return ParseCertificate(data)
}

// SignCertificate signs c with caPrivateKey and sets c.Signature. Used by the CA tool.
func SignCertificate(c *Certificate, caPrivateKey ed25519.PrivateKey) {
	msg := certSignedMessage(c)
	sig := ed25519.Sign(caPrivateKey, msg)
	c.Signature = base64.RawURLEncoding.EncodeToString(sig)
}

// normalizeURI applies the same normalization as chord.NormalizeURI without importing chord.
// Duplicated here to keep auth package free of internal/chord dependency.
func normalizeURI(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", errors.New("uri must use https scheme")
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("uri must be absolute https without userinfo, query, or fragment")
	}
	if u.Path != "" && u.Path != "/" {
		return "", errors.New("uri must not include a path")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", errors.New("uri host is required")
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if port == "443" {
		port = ""
	}
	if port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return "", errors.New("invalid uri port")
		}
		return "https://" + net.JoinHostPort(strings.Trim(host, "[]"), port), nil
	}
	return "https://" + host, nil
}
