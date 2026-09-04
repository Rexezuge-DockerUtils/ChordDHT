#!/usr/bin/env bash
# E2E integration test for ChordClient (peer node client).
#
# Spins up real `cmd/node` HTTPS binaries with a self-signed cert and
# exercises the same wire surface ChordClient uses:
#   Ping, FindSuccessor, Join, Rectify (+ /notify alias),
#   State (atomic snapshot), Predecessor, SuccessorList, Leave, RTT,
#   plus vnode per-node routing and auth-enforced paths.
#
# No external dependencies: go, openssl, curl, python3 only.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
NODE_BIN="$TMP/node"
CA_BIN="$TMP/ca"
CERT="$TMP/cert.pem"
KEY="$TMP/key.pem"

P1_URI="https://127.0.0.1:18441"
P1_LISTEN="127.0.0.1:18441"
P2_URI="https://127.0.0.1:18442"
P2_LISTEN="127.0.0.1:18442"
A1_URI="https://127.0.0.1:18443"
A1_LISTEN="127.0.0.1:18443"
A2_URI="https://127.0.0.1:18444"
A2_LISTEN="127.0.0.1:18444"

cleanup() {
  # Kill any nodes started by this script, then remove temp dir.
  jobs -p | xargs -r kill 2>/dev/null || true
  sleep 1
  jobs -p | xargs -r kill -9 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

msg() { echo "==> $*"; }
fail() { echo "E2E FAIL: $*" >&2; exit 1; }

wait_for() {
  # wait_for <url> [timeout_secs]
  local url="$1" timeout="${2:-20}" i
  for ((i = 0; i < timeout; i++)); do
    if curl -ksf "$url" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  return 1
}

json_get() {
  # json_get <url> <python-expr-on-d>  (d is parsed JSON)
  local url="$1" expr="$2"
  curl -ksf "$url" | python3 -c "import sys,json; d=json.load(sys.stdin); print($expr)"
}

msg "building node + ca binaries"
go build -o "$NODE_BIN" ./cmd/node
go build -o "$CA_BIN" ./tools/ca

msg "generating self-signed TLS cert (SAN: 127.0.0.1, localhost)"
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$KEY" -out "$CERT" -days 1 \
  -subj "/CN=127.0.0.1" \
  -addext "subjectAltName=IP:127.0.0.1,DNS:localhost" >/dev/null 2>&1

COMMON=(-skip-tls-verify -log-level info -http-timeout=5s
  -maintenance-interval=2s
  -stabilize-active-interval=2s -stabilize-quiet-interval=2s
  -fix-fingers-active-interval=2s -fix-fingers-quiet-interval=2s
  -check-predecessor-active-interval=2s -check-predecessor-quiet-interval=2s)

# ---------------------------------------------------------------- Phase 1: no-auth two-node ring
msg "phase 1: single-node boot $P1_URI"
"$NODE_BIN" -uri "$P1_URI" -listen "$P1_LISTEN" \
  -tls-cert "$CERT" -tls-key "$KEY" "${COMMON[@]}" >"$TMP/node1.log" 2>&1 &
wait_for "$P1_URI/chord/ping" 20 || { cat "$TMP/node1.log"; fail "node1 never came up"; }

[ "$(json_get "$P1_URI/chord/identity" "d['status']")" = "ACTIVE" ] || { cat "$TMP/node1.log"; fail "node1 identity not ACTIVE"; }
SELF1="$(json_get "$P1_URI/chord/identity" "d['node_id']")"
SUCC1="$(json_get "$P1_URI/chord/state" "d['successor']['node_id']")"
[ "$SUCC1" = "$SELF1" ] || fail "single-node successor should be self ($SUCC1 != $SELF1)"
[ "$(json_get "$P1_URI/chord/state" "d['successor_list_valid']")" = "True" ] || fail "single-node successor_list_valid should be true"
[ "$(json_get "$P1_URI/chord/invariant" "d['successor_list_valid']")" = "True" ] || fail "single-node invariant should be valid"
[ "$(curl -ksf "$P1_URI/chord/predecessor" | python3 -c "import sys,json; print(json.load(sys.stdin)['predecessor'])")" = "None" ] || fail "single-node predecessor should be null"
[ "$(json_get "$P1_URI/chord/finger_table" "len(d['finger_table'])")" = "160" ] || fail "finger_table should have 160 entries"
curl -ksf "$P1_URI/chord/rtt" >/dev/null || fail "GET /chord/rtt failed"
curl -ksf "$P1_URI/chord/status" >/dev/null || fail "GET /chord/status failed"
FOUND="$(curl -ksf -X POST "$P1_URI/chord/find_successor" -H 'Content-Type: application/json' \
  -d "{\"id\":\"$SELF1\",\"hop_count\":0,\"max_hops\":161}" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('found'), d['successor']['node_id'])")"
[ "$FOUND" = "True $SELF1" ] || fail "find_successor self lookup failed: $FOUND"
msg "phase 1 single-node checks passed (node_id=$SELF1)"

msg "phase 1: join second node $P2_URI via seeds"
"$NODE_BIN" -uri "$P2_URI" -listen "$P2_LISTEN" \
  -tls-cert "$CERT" -tls-key "$KEY" "${COMMON[@]}" \
  -seeds "$P1_URI" >"$TMP/node2.log" 2>&1 &
wait_for "$P2_URI/chord/ping" 20 || { cat "$TMP/node2.log"; fail "node2 never came up"; }

msg "waiting for 2-node ring stabilization"
RING_OK=0
for _ in $(seq 1 30); do
  S1=$(curl -ksf "$P1_URI/chord/state" | python3 -c "import sys,json; s=json.load(sys.stdin); print(s['successor']['uri'], (s['predecessor'] or {}).get('uri',''))" 2>/dev/null || echo "fail fail")
  S2=$(curl -ksf "$P2_URI/chord/state" | python3 -c "import sys,json; s=json.load(sys.stdin); print(s['successor']['uri'], (s['predecessor'] or {}).get('uri',''))" 2>/dev/null || echo "fail fail")
  if [ "$S1" = "$P2_URI $P2_URI" ] && [ "$S2" = "$P1_URI $P1_URI" ]; then RING_OK=1; break; fi
  sleep 1
done
[ "$RING_OK" = "1" ] || { echo "node1: $S1"; echo "node2: $S2"; fail "2-node ring did not stabilize"; }
msg "2-node ring stabilized"

SELF2="$(json_get "$P2_URI/chord/identity" "d['node_id']")"
# v5 atomic state fields (ChordClient.State surface).
for base in "$P1_URI" "$P2_URI"; do
  curl -ksf "$base/chord/state" | python3 -c "
import sys,json
s=json.load(sys.stdin)
assert s['successor_list_valid'] is True, 'successor_list_valid'
assert s['snapshot_timestamp'], 'snapshot_timestamp missing'
assert s['last_invariant_check'], 'last_invariant_check missing'
assert len(s['successor_list']) >= 1, 'successor_list empty'
" || fail "atomic State fields missing on $base"
  curl -ksf "$base/chord/invariant" | python3 -c "
import sys,json
s=json.load(sys.stdin)
assert s['successor_list_valid'] is True, s
assert s['violations'] == [], s['violations']
" || fail "invariant violations on $base"
  curl -ksf "$base/chord/predecessor" | python3 -c "
import sys,json
s=json.load(sys.stdin)
assert s['predecessor'] is not None, 'predecessor should be set after join'
" || fail "predecessor missing on $base"
done

# FindSuccessor across peers (iterative lookup surface).
curl -ksf -X POST "$P2_URI/chord/find_successor" -H 'Content-Type: application/json' \
  -d "{\"id\":\"$SELF1\",\"hop_count\":0,\"max_hops\":161}" | python3 -c "
import sys,json
d=json.load(sys.stdin)
assert d.get('successor') or d.get('next_hop'), d
" || fail "cross-node find_successor failed"

# Rectify + /notify alias (ChordClient.Rectify/Notify surface).
for op in rectify notify; do
  curl -ksf -X POST "$P1_URI/chord/$op" -H 'Content-Type: application/json' \
    -d "{\"node\":{\"node_id\":\"$SELF2\",\"uri\":\"$P2_URI\"}}" | python3 -c "
import sys,json
d=json.load(sys.stdin)
assert 'accepted' in d and 'predecessor' in d, d
" || fail "POST /chord/$op failed"
done
msg "rectify + notify alias checks passed"

# Join ID-collision negative (ChordClient.Join surface).
CODE=$(curl -k -s -o /tmp/e2e-join-collision.json -w "%{http_code}" -X POST "$P1_URI/chord/join" \
  -H 'Content-Type: application/json' -d "{\"node\":{\"node_id\":\"$SELF1\",\"uri\":\"$P1_URI\"}}")
[ "$CODE" = "409" ] || fail "join collision should be 409, got $CODE"
grep -q ID_COLLISION /tmp/e2e-join-collision.json || fail "join collision should report ID_COLLISION"

# Leave validation without disrupting the ring (ChordClient.Leave surface).
CODE=$(curl -k -s -o /dev/null -w "%{http_code}" -X POST "$P1_URI/chord/leave" \
  -H 'Content-Type: application/json' -d '{"role":"bogus"}')
[ "$CODE" = "400" ] || fail "leave with bogus role should be 400, got $CODE"
msg "phase 1 ChordClient checks passed"

# ---------------------------------------------------------------- Phase 2: auth + vnode routing
msg "phase 2: issuing auth credentials for $A1_URI and $A2_URI"
mkdir -p "$TMP/creds"
"$CA_BIN" gen-ca >"$TMP/ca.env" 2>"$TMP/ca.err"
CA_PRIV="$(grep CA_PRIVATE_KEY_BASE64 "$TMP/ca.env" | cut -d= -f2)"
CA_PUB="$(grep CA_PUBLIC_KEY_BASE64 "$TMP/ca.env" | cut -d= -f2)"
[ -n "$CA_PRIV" ] && [ -n "$CA_PUB" ] || fail "gen-ca did not emit keys"
"$CA_BIN" issue --ca-key="$CA_PRIV" --uri="$A1_URI" --out-dir="$TMP/creds" >/dev/null
"$CA_BIN" issue --ca-key="$CA_PRIV" --uri="$A2_URI" --out-dir="$TMP/creds" >/dev/null
A1_ID="$(python3 -c "import hashlib; print(hashlib.sha1(b'$A1_URI').hexdigest())")"
A2_ID="$(python3 -c "import hashlib; print(hashlib.sha1(b'$A2_URI').hexdigest())")"
[ -f "$TMP/creds/$A1_ID.cert.json" ] || fail "missing cert for $A1_URI (want $A1_ID)"
[ -f "$TMP/creds/$A2_ID.cert.json" ] || fail "missing cert for $A2_URI (want $A2_ID)"

msg "phase 2: starting auth+vnode node $A1_URI (vnode-count=2)"
"$NODE_BIN" -uri "$A1_URI" -listen "$A1_LISTEN" \
  -tls-cert "$CERT" -tls-key "$KEY" "${COMMON[@]}" \
  -vnode-count=2 \
  -auth.enabled "-auth.ca-public-key-base64=$CA_PUB" \
  "-auth.node-certificate-file=$TMP/creds/$A1_ID.cert.json" \
  "-auth.node-private-key-file=$TMP/creds/$A1_ID.privkey.b64" >"$TMP/anode1.log" 2>&1 &
wait_for "$A1_URI/chord/ping" 20 || { cat "$TMP/anode1.log"; fail "auth node1 never came up"; }
grep -q "joining vnode index=1" "$TMP/anode1.log" || fail "auth node1 did not spawn vnodes (need -auth.enabled for vnode proofs)"
grep -q "joining vnode index=2" "$TMP/anode1.log" || fail "auth node1 did not spawn vnode index=2"

# Exempt paths stay open without a signature.
for p in ping identity rtt status; do
  curl -ksf "$A1_URI/chord/$p" >/dev/null || fail "exempt /chord/$p should work without auth"
done
# Protected paths must reject unsigned callers (proves verifier is on).
CODE=$(curl -k -s -o /dev/null -w "%{http_code}" "$A1_URI/chord/state")
[ "$CODE" = "401" ] || fail "unsigned /chord/state should be 401, got $CODE"
CODE=$(curl -k -s -o /dev/null -w "%{http_code}" "$A1_URI/chord/node/$A1_ID/state")
[ "$CODE" = "401" ] || fail "unsigned per-node state should be 401, got $CODE"

# VNode per-node routing via exempt ping: real vnodes 200, unknown id 404.
V1="$(python3 -c "import hashlib; a='$A1_ID'; print(hashlib.sha1(f'chord-vnode-v4\n{a}\n1'.encode()).hexdigest())")"
V2="$(python3 -c "import hashlib; a='$A1_ID'; print(hashlib.sha1(f'chord-vnode-v4\n{a}\n2'.encode()).hexdigest())")"
for v in "$V1" "$V2"; do
  GOT="$(curl -ksf "$A1_URI/chord/node/$v/ping" | python3 -c "import sys,json; print(json.load(sys.stdin)['node_id'])")"
  [ "$GOT" = "$v" ] || fail "per-node ping for vnode $v returned $GOT"
done
CODE=$(curl -k -s -o /tmp/e2e-unknown-vnode.json -w "%{http_code}" "$A1_URI/chord/node/0000000000000000000000000000000000000000/ping")
[ "$CODE" = "404" ] || fail "unknown vnode ping should be 404, got $CODE"
grep -q NODE_NOT_FOUND /tmp/e2e-unknown-vnode.json || fail "unknown vnode ping should report NODE_NOT_FOUND"
msg "phase 2 vnode routing checks passed (v1=$V1 v2=$V2)"

msg "phase 2: joining second auth node $A2_URI via seeds (exercises signed ChordClient)"
"$NODE_BIN" -uri "$A2_URI" -listen "$A2_LISTEN" \
  -tls-cert "$CERT" -tls-key "$KEY" "${COMMON[@]}" \
  -vnode-count=1 \
  -auth.enabled "-auth.ca-public-key-base64=$CA_PUB" \
  "-auth.node-certificate-file=$TMP/creds/$A2_ID.cert.json" \
  "-auth.node-private-key-file=$TMP/creds/$A2_ID.privkey.b64" \
  -seeds "$A1_URI" >"$TMP/anode2.log" 2>&1 &
wait_for "$A2_URI/chord/ping" 20 || { cat "$TMP/anode2.log"; fail "auth node2 never came up"; }
JOINED=0
for _ in $(seq 1 20); do
  if grep -q "joined network via seed=" "$TMP/anode2.log"; then JOINED=1; break; fi
  sleep 1
done
[ "$JOINED" = "1" ] || { cat "$TMP/anode2.log"; fail "auth node2 did not join via signed ChordClient"; }
CODE=$(curl -k -s -o /dev/null -w "%{http_code}" "$A2_URI/chord/state")
[ "$CODE" = "401" ] || fail "auth node2 unsigned state should be 401, got $CODE"
A2V1="$(python3 -c "import hashlib; a='$A2_ID'; print(hashlib.sha1(f'chord-vnode-v4\n{a}\n1'.encode()).hexdigest())")"
GOT="$(curl -ksf "$A2_URI/chord/node/$A2V1/ping" | python3 -c "import sys,json; print(json.load(sys.stdin)['node_id'])")"
[ "$GOT" = "$A2V1" ] || fail "auth node2 vnode ping mismatch ($GOT != $A2V1)"
msg "phase 2 auth + signed-join checks passed"

msg "E2E ChordClient: ALL CHECKS PASSED"
