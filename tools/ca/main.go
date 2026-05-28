// ca is a standalone tool for managing Chord DHT v2.0 authentication credentials.
//
// Usage:
//
//	ca gen-ca
//	    Generates a new CA Ed25519 key pair and prints base64url-encoded
//	    private and public keys to stdout.
//
//	ca issue --ca-key=<base64url-private-64bytes> --uri=<https://...> [--days=365] [--out-dir=.]
//	    Issues a node certificate signed by the CA.
//	    Writes <node_id>.cert.json and <node_id>.privkey.b64 into --out-dir.
//
//	ca gen-crl --ca-key=<base64url-private> [--revoke=<node_id>,...] [--crl-in=<path>] [--out=crl.json]
//	    Creates or updates a CRL signed by the CA.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"chorddht/internal/auth"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "gen-ca":
		err = cmdGenCA(os.Args[2:])
	case "issue":
		err = cmdIssue(os.Args[2:])
	case "gen-crl":
		err = cmdGenCRL(os.Args[2:])
	default:
		printUsage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: ca <gen-ca|issue|gen-crl> [flags]")
	fmt.Fprintln(os.Stderr, "  gen-ca                                  Generate CA key pair")
	fmt.Fprintln(os.Stderr, "  issue --ca-key=<b64> --uri=<https://...> [--days=365] [--out-dir=.]")
	fmt.Fprintln(os.Stderr, "  gen-crl --ca-key=<b64> [--revoke=<id>,...] [--crl-in=<f>] [--out=crl.json]")
}

func cmdGenCA(args []string) error {
	fs := flag.NewFlagSet("gen-ca", flag.ExitOnError)
	_ = fs.Parse(args)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	privB64 := base64.RawURLEncoding.EncodeToString(priv)
	pubB64 := base64.RawURLEncoding.EncodeToString(pub)
	fmt.Printf("CA_PRIVATE_KEY_BASE64=%s\n", privB64)
	fmt.Printf("CA_PUBLIC_KEY_BASE64=%s\n", pubB64)
	fmt.Fprintln(os.Stderr, "Keep CA_PRIVATE_KEY_BASE64 offline and secret. Distribute CA_PUBLIC_KEY_BASE64 to all nodes.")
	return nil
}

func cmdIssue(args []string) error {
	fs := flag.NewFlagSet("issue", flag.ExitOnError)
	caKeyB64 := fs.String("ca-key", "", "CA Ed25519 private key (base64url, 64 bytes)")
	uri := fs.String("uri", "", "node https:// URI")
	days := fs.Int("days", 365, "certificate validity days")
	outDir := fs.String("out-dir", ".", "output directory")
	_ = fs.Parse(args)

	if *caKeyB64 == "" {
		return errors.New("--ca-key is required")
	}
	if *uri == "" {
		return errors.New("--uri is required")
	}

	caPrivBytes, err := base64.RawURLEncoding.DecodeString(*caKeyB64)
	if err != nil || len(caPrivBytes) != 64 {
		return errors.New("--ca-key must be base64url-encoded 64-byte Ed25519 private key")
	}
	caPrivKey := ed25519.PrivateKey(caPrivBytes)

	normalized, err := normalizeURI(*uri)
	if err != nil {
		return fmt.Errorf("invalid uri: %w", err)
	}
	sum := sha1.Sum([]byte(normalized))
	nodeID := hex.EncodeToString(sum[:])

	nodePub, nodePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate node key: %w", err)
	}

	now := time.Now()
	cert := auth.Certificate{
		Version:   1,
		NodeID:    nodeID,
		URI:       normalized,
		PublicKey: base64.RawURLEncoding.EncodeToString(nodePub),
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(time.Duration(*days) * 24 * time.Hour).Unix(),
	}
	auth.SignCertificate(&cert, caPrivKey)

	certJSON, err := json.MarshalIndent(cert, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cert: %w", err)
	}
	certPath := filepath.Join(*outDir, nodeID+".cert.json")
	if err := os.WriteFile(certPath, certJSON, 0o644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}

	privB64 := base64.RawURLEncoding.EncodeToString(nodePriv)
	privPath := filepath.Join(*outDir, nodeID+".privkey.b64")
	if err := os.WriteFile(privPath, []byte(privB64), 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	fmt.Printf("node_id:  %s\n", nodeID)
	fmt.Printf("cert:     %s\n", certPath)
	fmt.Printf("privkey:  %s\n", privPath)
	fmt.Printf("expires:  %s\n", time.Unix(cert.ExpiresAt, 0).UTC().Format(time.RFC3339))
	return nil
}

func cmdGenCRL(args []string) error {
	fs := flag.NewFlagSet("gen-crl", flag.ExitOnError)
	caKeyB64 := fs.String("ca-key", "", "CA Ed25519 private key (base64url, 64 bytes)")
	revoke := fs.String("revoke", "", "comma-separated node_ids to add to revocation list")
	crlIn := fs.String("crl-in", "", "existing CRL JSON file to update (optional)")
	out := fs.String("out", "crl.json", "output CRL file path")
	_ = fs.Parse(args)

	if *caKeyB64 == "" {
		return errors.New("--ca-key is required")
	}
	caPrivBytes, err := base64.RawURLEncoding.DecodeString(*caKeyB64)
	if err != nil || len(caPrivBytes) != 64 {
		return errors.New("--ca-key must be base64url-encoded 64-byte Ed25519 private key")
	}
	caPrivKey := ed25519.PrivateKey(caPrivBytes)

	var crl auth.CRL
	if *crlIn != "" {
		data, err := os.ReadFile(*crlIn)
		if err != nil {
			return fmt.Errorf("read crl-in: %w", err)
		}
		if err := json.Unmarshal(data, &crl); err != nil {
			return fmt.Errorf("parse crl-in: %w", err)
		}
		crl.Version++
	} else {
		crl.Version = 1
	}
	crl.UpdatedAt = time.Now().Unix()

	seen := make(map[string]bool)
	for _, id := range crl.RevokedIDs {
		seen[id] = true
	}
	if *revoke != "" {
		for _, id := range strings.Split(*revoke, ",") {
			id = strings.TrimSpace(id)
			if id != "" && !seen[id] {
				crl.RevokedIDs = append(crl.RevokedIDs, id)
				seen[id] = true
			}
		}
	}

	auth.SignCRL(&crl, caPrivKey)

	data, err := json.MarshalIndent(crl, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal crl: %w", err)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return fmt.Errorf("write crl: %w", err)
	}
	fmt.Printf("crl written: %s (version=%d revoked=%d)\n", *out, crl.Version, len(crl.RevokedIDs))
	return nil
}

// normalizeURI applies the same rules as chord.NormalizeURI (duplicated to avoid import).
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
