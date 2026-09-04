#!/usr/bin/env bash
# E2E integration test for TrackerClient (tracker outbound client).
#
# Spins up a real `cmd/node` HTTPS binary pointed at a minimal fake HTTPS
# tracker (.github/scripts/fake-tracker.py) and verifies the TrackerClient
# wire surface: Seeds, Register, Heartbeat, Deregister, DetectRegion (/geo),
# plus FetchCRL reachability. Kept separate from e2e-chord-client.sh so
# peer-client and tracker-client failures are independently visible in CI.
#
# No external dependencies: go, openssl, curl, python3 only.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
NODE_BIN="$TMP/node"
CERT="$TMP/cert.pem"
KEY="$TMP/key.pem"
TRACKER_LOG="$TMP/tracker-requests.log"

NODE_URI="https://127.0.0.1:18641"
NODE_LISTEN="127.0.0.1:18641"
TRACKER_URL="https://127.0.0.1:18650"

FAKE_PID=""
NODE_PID=""

cleanup() {
  if [ -n "$NODE_PID" ] && kill -0 "$NODE_PID" 2>/dev/null; then kill "$NODE_PID" 2>/dev/null || true; fi
  if [ -n "$FAKE_PID" ] && kill -0 "$FAKE_PID" 2>/dev/null; then kill "$FAKE_PID" 2>/dev/null || true; fi
  sleep 1
  if [ -n "$NODE_PID" ] && kill -0 "$NODE_PID" 2>/dev/null; then kill -9 "$NODE_PID" 2>/dev/null || true; fi
  if [ -n "$FAKE_PID" ] && kill -0 "$FAKE_PID" 2>/dev/null; then kill -9 "$FAKE_PID" 2>/dev/null || true; fi
  rm -rf "$TMP"
}
trap cleanup EXIT

msg() { echo "==> $*"; }
fail() { echo "E2E FAIL: $*" >&2; exit 1; }

wait_for() {
  local url="$1" timeout="${2:-20}" i
  for ((i = 0; i < timeout; i++)); do
    if curl -ksf "$url" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  return 1
}

wait_for_log() {
  # wait_for_log <grep-pattern> [timeout_secs]
  local pattern="$1" timeout="${2:-20}" i
  for ((i = 0; i < timeout; i++)); do
    if grep -qE "$pattern" "$TRACKER_LOG" 2>/dev/null; then return 0; fi
    sleep 1
  done
  return 1
}

msg "building node binary"
go build -o "$NODE_BIN" ./cmd/node

msg "generating self-signed TLS cert"
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$KEY" -out "$CERT" -days 1 \
  -subj "/CN=127.0.0.1" \
  -addext "subjectAltName=IP:127.0.0.1,DNS:localhost" >/dev/null 2>&1

msg "starting fake tracker at $TRACKER_URL"
python3 "$ROOT/.github/scripts/fake-tracker.py" \
  --port 18650 --cert "$CERT" --key "$KEY" --log "$TRACKER_LOG" >"$TMP/fake.log" 2>&1 &
FAKE_PID=$!
wait_for "$TRACKER_URL/tracker/nodes/seeds?count=5" 20 || { cat "$TMP/fake.log"; fail "fake tracker never came up"; }

# Direct TrackerClient wire checks (same paths TrackerClient uses).
msg "verifying tracker wire surface directly (Seeds/Register/Heartbeat/geo/crl)"
curl -ksf "$TRACKER_URL/tracker/nodes/seeds?count=5&exclude=abc&include_cert=true" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); assert 'seeds' in d, d" \
  || fail "GET /tracker/nodes/seeds failed"
curl -ksf -X POST "$TRACKER_URL/tracker/nodes" -H 'Content-Type: application/json' \
  -d '{"node_id":"0000000000000000000000000000000000000000","uri":"https://127.0.0.1:18641"}' \
  | python3 -c "import sys,json; d=json.load(sys.stdin); assert d.get('region')=='e2e-region', d" \
  || fail "POST /tracker/nodes failed"
curl -ksf -X POST "$TRACKER_URL/tracker/nodes/0000000000000000000000000000000000000000/heartbeat" \
  -H 'Content-Type: application/json' -d '{"status":"ACTIVE"}' >/dev/null \
  || fail "POST heartbeat failed"
curl -ksf "$TRACKER_URL/tracker/geo" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); assert d.get('region')=='e2e-region', d" \
  || fail "GET /tracker/geo failed"
curl -ksf "$TRACKER_URL/tracker/crl" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); assert 'version' in d, d" \
  || fail "GET /tracker/crl failed (FetchCRL surface)"
# Reset log so subsequent assertions only reflect node-driven traffic.
: >"$TRACKER_LOG"

msg "starting node with -tracker-url $TRACKER_URL"
"$NODE_BIN" -uri "$NODE_URI" -listen "$NODE_LISTEN" \
  -tls-cert "$CERT" -tls-key "$KEY" \
  -skip-tls-verify -log-level info -http-timeout=5s \
  -maintenance-interval=2s \
  -tracker-url "$TRACKER_URL" -tracker-seed-count=5 \
  -tracker-heartbeat-active-interval=2s -tracker-heartbeat-quiet-interval=2s \
  -tracker-crl-interval=2s >"$TMP/node.log" 2>&1 &
NODE_PID=$!
wait_for "$NODE_URI/chord/ping" 20 || { cat "$TMP/node.log"; fail "node never came up"; }

msg "verifying Register + Seeds + DetectRegion via fake-tracker log"
wait_for_log "POST /tracker/nodes body=" 20 || { cat "$TRACKER_LOG"; cat "$TMP/node.log"; fail "tracker never saw Register (POST /tracker/nodes)"; }
wait_for_log "GET /tracker/nodes/seeds.*include_cert=true" 20 || { cat "$TRACKER_LOG"; fail "tracker never saw Seeds (GET /tracker/nodes/seeds)"; }
wait_for_log "GET /tracker/geo" 20 || { cat "$TRACKER_LOG"; fail "tracker never saw DetectRegion (GET /tracker/geo)"; }
grep -q "e2e-region" "$TRACKER_LOG" || fail "Register body should carry auto-detected region"

# Region auto-detection is the user-visible effect of DetectRegion+Register.
REGION="$(curl -ksf "$NODE_URI/chord/state" | python3 -c "import sys,json; print(json.load(sys.stdin).get('region',''))")"
[ "$REGION" = "e2e-region" ] || { cat "$TMP/node.log"; fail "node region should be e2e-region, got '$REGION'"; }
msg "region auto-detected: $REGION"

msg "verifying Heartbeat loop (expect >=2 heartbeats; tracker loop ticks every 5s)"
wait_for_log "POST /tracker/nodes/[0-9a-f]{40}/heartbeat" 20 || { cat "$TRACKER_LOG"; fail "tracker never saw Heartbeat"; }
# The node sends one heartbeat immediately on startup and then on a 5s ticker
# (throttled by the heartbeat interval flags), so allow up to ~15s for the 2nd.
HB_OK=0
for _ in $(seq 1 15); do
  COUNT=$(grep -cE "POST /tracker/nodes/[0-9a-f]{40}/heartbeat" "$TRACKER_LOG" || true)
  if [ "$COUNT" -ge 2 ]; then HB_OK=1; break; fi
  sleep 1
done
[ "$HB_OK" = "1" ] || { cat "$TRACKER_LOG"; fail "expected >=2 heartbeats, saw $COUNT"; }
grep "POST /tracker/nodes/" "$TRACKER_LOG" | head -1 | grep -q '"status":"ACTIVE"' \
  || fail "heartbeat body should contain status ACTIVE"
msg "heartbeat loop verified ($COUNT heartbeats)"

msg "verifying Deregister on graceful shutdown (SIGTERM)"
NODE_ID="$(curl -ksf "$NODE_URI/chord/identity" | python3 -c "import sys,json; print(json.load(sys.stdin)['node_id'])")"
kill -TERM "$NODE_PID"
wait "$NODE_PID" 2>/dev/null || true
NODE_PID=""
sleep 1
grep -qE "DELETE /tracker/nodes/$NODE_ID" "$TRACKER_LOG" \
  || { cat "$TRACKER_LOG"; fail "tracker never saw Deregister (DELETE /tracker/nodes/$NODE_ID)"; }
msg "deregister verified for $NODE_ID"

msg "E2E TrackerClient: ALL CHECKS PASSED"
