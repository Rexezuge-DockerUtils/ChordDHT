package chord

import (
	"crypto/ed25519"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// VNodeProof is a signed credential that proves a vnode_id is legitimately derived
// from a given anchor. It is signed by the anchor's Ed25519 private key.
type VNodeProof struct {
	VNodeID   string `json:"vnode_id"`
	AnchorID  string `json:"anchor_id"`
	Index     int    `json:"index"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
	AnchorPub string `json:"anchor_pub"` // base64 std, 32-byte Ed25519 public key
	Signature string `json:"signature"`  // base64 std, 64-byte Ed25519 signature
}

// DeriveVNodeID deterministically computes a vnode_id from an anchor_id and index.
// Formula: SHA1("chord-vnode-v4\n" + anchor_id + "\n" + strconv.Itoa(index))
func DeriveVNodeID(anchorID string, index int) string {
	input := fmt.Sprintf("chord-vnode-v4\n%s\n%d", anchorID, index)
	h := sha1.Sum([]byte(input))
	return hex.EncodeToString(h[:])
}

func buildProofCanonical(p *VNodeProof) string {
	return fmt.Sprintf("chord-vnode-proof-v4\n%s\n%s\n%d\n%d\n%d",
		p.VNodeID, p.AnchorID, p.Index, p.IssuedAt, p.ExpiresAt)
}

// SignVNodeProof creates and signs a VNodeProof for the given index using the anchor's private key.
func SignVNodeProof(anchorID string, index int, privKey ed25519.PrivateKey, ttl time.Duration) *VNodeProof {
	vnodeID := DeriveVNodeID(anchorID, index)
	now := time.Now().Unix()
	pubKey := privKey.Public().(ed25519.PublicKey)
	proof := &VNodeProof{
		VNodeID:   vnodeID,
		AnchorID:  anchorID,
		Index:     index,
		IssuedAt:  now,
		ExpiresAt: now + int64(ttl.Seconds()),
		AnchorPub: base64.StdEncoding.EncodeToString(pubKey),
	}
	canonical := buildProofCanonical(proof)
	sig := ed25519.Sign(privKey, []byte(canonical))
	proof.Signature = base64.StdEncoding.EncodeToString(sig)
	return proof
}

// VerifyVNodeProof verifies a VNodeProof against the anchor's Ed25519 public key.
// clockSkew is added to the expiry deadline to tolerate clock drift between peers.
func VerifyVNodeProof(proof *VNodeProof, anchorPubKey ed25519.PublicKey, clockSkew time.Duration) error {
	now := time.Now().Unix()
	if proof.ExpiresAt+int64(clockSkew.Seconds()) < now {
		return NewAPIError(http.StatusForbidden, ErrProofExpired, "vnode proof has expired")
	}
	expected := DeriveVNodeID(proof.AnchorID, proof.Index)
	if expected != proof.VNodeID {
		return NewAPIError(http.StatusForbidden, ErrInvalidVNodeProof, "vnode_id does not match derived value")
	}
	canonical := buildProofCanonical(proof)
	sig, err := base64.StdEncoding.DecodeString(proof.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return NewAPIError(http.StatusForbidden, ErrInvalidVNodeProof, "malformed proof signature")
	}
	if !ed25519.Verify(anchorPubKey, []byte(canonical), sig) {
		return NewAPIError(http.StatusForbidden, ErrInvalidVNodeProof, "proof signature verification failed")
	}
	return nil
}
