package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// CRL is the Certificate Revocation List format signed by the CA.
type CRL struct {
	Version    int      `json:"version"`
	UpdatedAt  int64    `json:"updated_at"`
	RevokedIDs []string `json:"revoked_node_ids"`
	Signature  string   `json:"signature"` // base64url CA signature
}

// crlSignedMessage builds the canonical signing input for a CRL.
// Format: "chord-crl-v1\nversion={}\nupdated_at={}\nrevoked_node_ids={sorted,comma-separated}"
func crlSignedMessage(c *CRL) []byte {
	sorted := make([]string, len(c.RevokedIDs))
	copy(sorted, c.RevokedIDs)
	sort.Strings(sorted)
	return []byte(fmt.Sprintf(
		"chord-crl-v1\nversion=%d\nupdated_at=%d\nrevoked_node_ids=%s",
		c.Version, c.UpdatedAt, strings.Join(sorted, ","),
	))
}

// VerifyCRL checks that the CRL's CA signature is valid.
func VerifyCRL(c *CRL, caPublicKey ed25519.PublicKey) error {
	if c.Version < 1 {
		return errors.New("crl version must be >= 1")
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(c.Signature)
	if err != nil || len(sigBytes) != 64 {
		return errors.New("crl signature must be base64url-encoded 64-byte Ed25519 signature")
	}
	msg := crlSignedMessage(c)
	if !ed25519.Verify(caPublicKey, msg, sigBytes) {
		return errors.New("crl CA signature verification failed")
	}
	return nil
}

// LoadCRLFromFile reads and verifies a CRL JSON file.
func LoadCRLFromFile(path string, caPublicKey ed25519.PublicKey) (*CRL, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read crl file: %w", err)
	}
	var c CRL
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse crl: %w", err)
	}
	if err := VerifyCRL(&c, caPublicKey); err != nil {
		return nil, err
	}
	return &c, nil
}

// ParseCRL decodes CRL from JSON bytes and verifies it.
func ParseCRL(data []byte, caPublicKey ed25519.PublicKey) (*CRL, error) {
	var c CRL
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse crl: %w", err)
	}
	if err := VerifyCRL(&c, caPublicKey); err != nil {
		return nil, err
	}
	return &c, nil
}

// IsRevoked reports whether nodeID is in the revocation list.
func (c *CRL) IsRevoked(nodeID string) bool {
	if c == nil {
		return false
	}
	for _, id := range c.RevokedIDs {
		if id == nodeID {
			return true
		}
	}
	return false
}

// SignCRL signs the CRL with the CA private key and sets Signature. Used by the CA tool.
func SignCRL(c *CRL, caPrivateKey ed25519.PrivateKey) {
	msg := crlSignedMessage(c)
	sig := ed25519.Sign(caPrivateKey, msg)
	c.Signature = base64.RawURLEncoding.EncodeToString(sig)
}
