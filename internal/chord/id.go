package chord

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"math/big"
	"net"
	"net/url"
	"strings"
)

var ringSize = new(big.Int).Lsh(big.NewInt(1), DefaultM)

func NormalizeURI(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", errors.New("uri must use https scheme")
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("uri must be an absolute https URI without userinfo, query, or fragment")
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

func HashURI(uri string) (string, error) {
	normalized, err := NormalizeURI(uri)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(normalized))
	return hex.EncodeToString(sum[:]), nil
}

func ValidateID(id string) bool {
	if len(id) != 40 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func idToBig(id string) (*big.Int, error) {
	if !ValidateID(id) {
		return nil, errors.New("invalid node id")
	}
	n := new(big.Int)
	n.SetString(id, 16)
	return n, nil
}

func bigToID(n *big.Int) string {
	return strings.ToLower(strings.Repeat("0", 40-len(n.Text(16))) + n.Text(16))
}

func FingerStart(selfID string, index int) (string, error) {
	if index < 0 || index >= DefaultM {
		return "", errors.New("finger index out of range")
	}
	self, err := idToBig(selfID)
	if err != nil {
		return "", err
	}
	offset := new(big.Int).Lsh(big.NewInt(1), uint(index))
	start := new(big.Int).Add(self, offset)
	start.Mod(start, ringSize)
	return bigToID(start), nil
}

func InRangeOpenClosed(xID, aID, bID string) bool {
	x, err1 := idToBig(xID)
	a, err2 := idToBig(aID)
	b, err3 := idToBig(bID)
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	if a.Cmp(b) < 0 {
		return x.Cmp(a) > 0 && x.Cmp(b) <= 0
	}
	if a.Cmp(b) > 0 {
		return x.Cmp(a) > 0 || x.Cmp(b) <= 0
	}
	return true
}

func InRangeOpenOpen(xID, aID, bID string) bool {
	x, err1 := idToBig(xID)
	a, err2 := idToBig(aID)
	b, err3 := idToBig(bID)
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	if a.Cmp(b) < 0 {
		return x.Cmp(a) > 0 && x.Cmp(b) < 0
	}
	if a.Cmp(b) > 0 {
		return x.Cmp(a) > 0 || x.Cmp(b) < 0
	}
	return x.Cmp(a) != 0
}
