package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chorddht/internal/chord"
)

func heartbeatTestServer(t *testing.T, response string, check func(hb chord.TrackerHeartbeat)) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/heartbeat") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var hb chord.TrackerHeartbeat
		if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
			t.Errorf("decode heartbeat body: %v", err)
		}
		if check != nil {
			check(hb)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
}

func TestHeartbeatSendsCRLVersionAndDecodesPiggyback(t *testing.T) {
	crl := `{"version":8,"updated_at":1780000000,"revoked_node_ids":[],"signature":"sig"}`
	server := heartbeatTestServer(t,
		`{"acknowledged":true,"tracker_time":"2026-09-16T00:00:00Z","crl_version":8,"crl":`+crl+`}`,
		func(hb chord.TrackerHeartbeat) {
			if hb.CRLVersion == nil || *hb.CRLVersion != 7 {
				t.Errorf("expected crl_version=7 in request, got %+v", hb.CRLVersion)
			}
		})
	defer server.Close()

	c, err := NewTrackerClient(server.URL, time.Second, true)
	if err != nil {
		t.Fatal(err)
	}
	version := 7
	resp, err := c.Heartbeat(strings.Repeat("a", 40), chord.TrackerHeartbeat{Status: chord.StatusActive, CRLVersion: &version})
	if err != nil {
		t.Fatal(err)
	}
	if resp.CRLVersion == nil || *resp.CRLVersion != 8 {
		t.Fatalf("expected crl_version=8 in response, got %+v", resp.CRLVersion)
	}
	if string(resp.CRL) != crl {
		t.Fatalf("expected inline CRL %s, got %s", crl, resp.CRL)
	}
}

func TestHeartbeatToleratesLegacyTrackerResponse(t *testing.T) {
	server := heartbeatTestServer(t,
		`{"acknowledged":true,"tracker_time":"2026-09-16T00:00:00Z"}`,
		func(hb chord.TrackerHeartbeat) {
			if hb.CRLVersion != nil {
				t.Errorf("expected no crl_version in request, got %d", *hb.CRLVersion)
			}
		})
	defer server.Close()

	c, err := NewTrackerClient(server.URL, time.Second, true)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Heartbeat(strings.Repeat("a", 40), chord.TrackerHeartbeat{Status: chord.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Acknowledged {
		t.Fatal("expected acknowledged=true")
	}
	if resp.CRLVersion != nil {
		t.Fatalf("expected nil crl_version from legacy tracker, got %d", *resp.CRLVersion)
	}
	if len(resp.CRL) != 0 {
		t.Fatalf("expected no CRL from legacy tracker, got %s", resp.CRL)
	}
}
