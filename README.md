# ChordDHT

A learning implementation of the [Chord](https://pdos.csail.mit.edu/papers/ton:chord/paper-ton.pdf) distributed hash table protocol (Stoica et al., ACM SIGCOMM 2001), written in Go with no external dependencies. Now at **v5.0** with the Zave protocol corrections from *How to Make Chord Correct*: Rectify, atomic state-based Stabilize, candidate liveness checks, correct Join successor-list initialization, successor-list invariant tracking, and stable-base metadata.

> **Scope:** Chord ring formation and O(log N) key routing only. No key-value storage. Not intended for production use.

## Sister Project

[**ChordDHT-Tracker**](https://github.com/Rexezuge-CloudflareWorkers/ChordDHT-Tracker) — the optional bootstrap tracker for this node, implemented as a Cloudflare Worker with a D1 database and a React ring-visualization dashboard.

[**ChordDHT-Design**](https://github.com/Rexezuge-Gists/ChordDHT-Design) — design documentation for the ChordDHT system.

## How It Works

Every node maps its canonical HTTPS URI to a 160-bit ID via SHA-1 and joins a consistent-hash ring. Routing uses an iterative finger-table lookup that resolves any key in at most O(log N) hops. Independent maintenance goroutines run at adaptive intervals to keep the ring self-healing — faster during topology changes, slower during steady state.

Key protocol parameters:

| Parameter | Value | Notes |
|---|---|---|
| ID space | 2¹⁶⁰ (SHA-1) | Matches the original Chord paper |
| Finger table | 160 entries | Batch-parallel repair (k=8 active / k=4 quiet) |
| Successor list | r = 5 | Tolerates up to 4 consecutive node failures |
| Stable base | r + 1 physical anchors | VNodes do not count toward the stable-base minimum |
| Stabilize interval | 15 s (active) / 60 s (quiet) | Switches on topology events |
| fix_fingers interval | 10 s (active) / 30 s (quiet) | Exponential-jump repair order |
| Lookup mode | Iterative + LRU cache | HTTP depth always 1; optional parallel probe |
| Routing cache | LRU, 1000 entries, 30 s TTL | Interval-aware; cleared on topology change |
| Region routing | EWMA RTT + region affinity | Score = 0.6·ID + 0.3·RTT + 0.1·region |

### v5.0 additions over v4.0

- **Rectify replaces Notify semantics** — `POST /chord/rectify` checks whether the current predecessor is alive before rejecting a candidate by ID range. `POST /chord/notify` remains a compatibility alias by default.
- **Atomic Stabilize state RPC** — Stabilize uses `GET /chord/state` to read a successor's predecessor and successor list from one snapshot before making successor decisions.
- **Candidate liveness validation** — Stabilize pings a candidate predecessor before adopting it as the new successor, preventing dead predecessor references from entering the successor chain.
- **Correct Join successor-list initialization** — a joining node builds `[successor] + successor.successor_list[:r-1]` before becoming active.
- **Ordered successor-list checks** — every Stabilize can validate, trim, and extend the successor list; `/chord/state` reports `successor_list_valid`, `last_invariant_check`, and `snapshot_timestamp`.
- **Invariant diagnostics** — `GET /chord/invariant` returns successor-list validity and violation details for debugging.
- **Stable-base metadata** — nodes can be configured with stable-base anchor URIs and expose `is_stable_base_member` in state. The tracker monitors stable-base liveness on demand.

### v4.0 additions over v3.0

- **Virtual nodes (VNodes)** — a physical node can occupy N positions in the ring (`--vnode-count=N`; `N=0` is pure anchor mode, fully backwards-compatible)
- **VNodeProof credentials** — each vnode carries a short-lived Ed25519-signed proof derived from the anchor's certificate; no CA involvement for vnodes
- **Deterministic vnode IDs** — `SHA1("chord-vnode-v4\n" + anchor_id + "\n" + index)` — survives restarts
- **Per-node URL routing** — `/chord/node/{node_id}/` prefix routes requests to the correct vnode's state machine; old `/chord/` paths remain as anchor aliases
- **Shared L0 resources** — RTT cache, routing cache, and NodeInfo cache are shared across all vnodes on one host; finger tables store only 40-char node_id strings (6.4 KB vs 160 KB per vnode)
- **Sibling diversity constraint** — successor list caps entries from the same anchor at 50% to reduce cascading failures
- **Staggered vnode maintenance** — startup offsets spread maintenance load across the host
- **Graceful leave ordering** — vnodes leave in reverse-index order before the anchor
- **New endpoints** — `GET /chord/node/{id}/vnode_info`, `GET /chord/node/{anchor_id}/list_vnodes`, `POST .../transfer_keys`, `POST .../transfer_ack`

### v3.0 additions over v2.0

- **Adaptive maintenance** — `ACTIVE_MAINTENANCE` / `QUIET_MAINTENANCE` modes with separate goroutine loops
- **Batch parallel fix_fingers** — repairs k entries concurrently in exponential-jump priority order
- **Fast crash detection** — predecessor confirmed dead after 2 retries; immediate successor switch on failure
- **Dynamic successor list** — default r=5, configurable up to 10
- **Predecessor chain** — `predecessor_list[2]` for faster stabilization after predecessor loss
- **Multi-path isolation recovery** — successor list → predecessor list → finger scan → tracker → single-node fallback
- **LRU routing cache** — skip full ring traversal for hot keys; interval-aware cache entries
- **Latency-aware routing** — RTT-weighted EWMA + region affinity score replaces pure ID distance
- **Tiered timeouts** — different same-region / cross-region timeouts per operation type
- **Piggyback hints** — RTT and successor list hints ride existing response bodies
- **Finger table warm-up** — concurrently fetches 32 entries immediately after join
- **New endpoints** — `GET /chord/rtt` and `GET /chord/status` for observability

## Project Layout

```
cmd/node/           # Executable — starts an HTTPS Chord node
internal/auth/      # v2.0 identity authentication (cert, signer, verifier, CRL, nonce/cert caches)
internal/chord/     # Ring state, lookup, join/leave, stabilization, finger repair
internal/httpapi/   # JSON HTTP wrapper around chord.Node
internal/client/    # Outbound HTTP clients for peer and tracker
internal/config/    # CLI flags + environment variable loading
internal/logging/   # Levelled logger
tools/ca/           # Standalone CA tool: gen-ca, issue, gen-crl subcommands
```

## Building

```sh
go build ./cmd/node
go build ./tools/ca   # CA tool for credential management
```

Binaries for `linux/amd64` and `linux/arm64` are published as GitHub release artifacts on every `v*` tag.

## Running a Node

The node is **strict HTTPS only**. A TLS cert and key are required even for local testing (e.g. a self-signed cert).

```sh
node \
  -uri          https://node1.example.com \
  -tls-cert     /path/to/cert.pem \
  -tls-key      /path/to/key.pem
```

Every flag has an environment variable equivalent:

| Flag | Env var | Default | Description |
|---|---|---|---|
| `-uri` | `NODE_URI` | *(required)* | Canonical HTTPS URI for this node. Determines `node_id = SHA1(uri)`. Port 443 is stripped; host is lowercased. |
| `-listen` | `NODE_LISTEN` | URI port | `host:port` to bind the TLS server (defaults to the port in `-uri`) |
| `-tls-cert` | `NODE_TLS_CERT_FILE` | *(required)* | TLS certificate file |
| `-tls-key` | `NODE_TLS_KEY_FILE` | *(required)* | TLS private key file |
| `-skip-tls-verify` | `CHORD_SKIP_TLS_VERIFY` | `false` | Disable outbound peer/tracker TLS verification. The local server still requires a cert. |
| `-log-level` | `CHORD_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `-tracker-url` | `TRACKER_URL` | *(none)* | Optional tracker HTTPS URI for bootstrap and heartbeat |
| `-seeds` | `NODE_MANUAL_SEEDS` | *(none)* | Comma-separated seed node URIs used when no tracker is configured |
| `-http-timeout` | `CHORD_HTTP_TIMEOUT_SECONDS`¹ | `5s` | Outbound HTTP request timeout |
| `-maintenance-interval` | `CHORD_MAINTENANCE_INTERVAL_SECONDS`¹ | `60s` | How often the maintenance cycle runs |
| `-successor-list-size` | `CHORD_SUCCESSOR_LIST_SIZE` | `5` | Successor list length (r) |
| `-max-hops` | `CHORD_MAX_HOPS` | `161` | Hard hop limit for `find_successor` |
| `-suspicious-threshold` | `CHORD_SUSPICIOUS_THRESHOLD` | `1` | Consecutive failures before marking a peer suspicious |
| `-failed-threshold` | `CHORD_FAILED_THRESHOLD` | `3` | Consecutive failures before evicting a peer from routing tables |
| `-tracker-seed-count` | `TRACKER_SEED_COUNT` | `5` | How many seed nodes to request from the tracker on join |
| `-ping-liveness-timeout` | `CHORD_PING_LIVENESS_TIMEOUT_SECONDS`¹ | `2s` | Dedicated timeout for v5 liveness pings used by Stabilize/Rectify |
| `-stabilize-atomic-state` | `CHORD_STABILIZE_ATOMIC_STATE` | `true` | Use `GET /state` snapshots for Stabilize and Join successor-list reads |
| `-validate-after-stabilize` | `CHORD_VALIDATE_AFTER_STABILIZE` | `true` | Validate and repair the successor list after Stabilize |
| `-rectify-endpoint-alias` | `CHORD_RECTIFY_ENDPOINT_ALIAS` | `true` | Keep `/notify` as a compatibility alias for `/rectify` |
| `-invariant-audit-interval` | `CHORD_INVARIANT_AUDIT_INTERVAL_SECONDS`¹ | `300s` | Periodic invariant audit log interval; set `0` to disable |
| `-stable-base-min-size` | `CHORD_STABLE_BASE_MIN_SIZE` | `r+1` | Expected minimum stable-base physical anchor count |
| `-stable-base-members` | `CHORD_STABLE_BASE_MEMBERS` | *(none)* | Comma-separated stable-base anchor HTTPS URIs |

¹ Environment duration values are **integer seconds** (e.g. `CHORD_HTTP_TIMEOUT_SECONDS=10`). CLI flags use Go duration syntax (e.g. `-http-timeout=10s`).

### v3.0 Tuning Flags

Most v3.0 features are enabled by default. Parallel lookup is opt-in.

| Flag | Env var | Default | Description |
|---|---|---|---|
| `-node-region` | `CHORD_NODE_REGION` | `default` | Region label used for latency-aware routing and timeout selection |
| `-predecessor-list-size` | `CHORD_PREDECESSOR_LIST_SIZE` | `2` | Predecessor chain backup length |
| `-fix-fingers-batch-active` | `CHORD_FIX_FINGERS_BATCH_ACTIVE` | `8` | Finger entries repaired per cycle in active mode |
| `-fix-fingers-batch-quiet` | `CHORD_FIX_FINGERS_BATCH_QUIET` | `4` | Finger entries repaired per cycle in quiet mode |
| `-routing-cache-enabled` | `CHORD_ROUTING_CACHE_ENABLED` | `true` | Enable LRU routing result cache |
| `-routing-cache-size` | `CHORD_ROUTING_CACHE_SIZE` | `1000` | Maximum routing cache entries |
| `-routing-cache-ttl` | `CHORD_ROUTING_CACHE_TTL_SECONDS`¹ | `30s` | Routing cache entry TTL |
| `-latency-weight-id` | `CHORD_LATENCY_WEIGHT_ID` | `0.6` | Routing score weight for ID proximity |
| `-latency-weight-rtt` | `CHORD_LATENCY_WEIGHT_RTT` | `0.3` | Routing score weight for RTT |
| `-latency-weight-region` | `CHORD_LATENCY_WEIGHT_REGION` | `0.1` | Routing score weight for region affinity |
| `-parallel-lookup-enabled` | `CHORD_PARALLEL_LOOKUP_ENABLED` | `false` | Enable parallel `find_successor` probing |
| `-parallel-lookup-candidates` | `CHORD_PARALLEL_LOOKUP_CANDIDATES` | `3` | Candidate count for parallel lookup |
| `-timeout-ping-same` | `CHORD_TIMEOUT_PING_SAME`¹ | `2s` | `/ping` timeout for same-region peers |
| `-timeout-ping-cross` | `CHORD_TIMEOUT_PING_CROSS`¹ | `5s` | `/ping` timeout for cross-region peers |
| `-timeout-find-successor-same` | `CHORD_TIMEOUT_FIND_SUCCESSOR_SAME`¹ | `5s` | `/find_successor` timeout for same-region peers |
| `-timeout-find-successor-cross` | `CHORD_TIMEOUT_FIND_SUCCESSOR_CROSS`¹ | `15s` | `/find_successor` timeout for cross-region peers |
| `-timeout-fix-fingers-same` | `CHORD_TIMEOUT_FIX_FINGERS_SAME`¹ | `5s` | `fix_fingers` lookup timeout for same-region peers |
| `-timeout-fix-fingers-cross` | `CHORD_TIMEOUT_FIX_FINGERS_CROSS`¹ | `30s` | `fix_fingers` lookup timeout for cross-region peers |
| `-latency-probe-interval-active` | `CHORD_LATENCY_PROBE_ACTIVE`¹ | `30s` | RTT probe interval in active mode |
| `-latency-probe-interval-quiet` | `CHORD_LATENCY_PROBE_QUIET`¹ | `120s` | RTT probe interval in quiet mode |
| `-rtt-ewma-alpha` | `CHORD_RTT_EWMA_ALPHA` | `0.3` | EWMA smoothing factor for RTT samples |
| `-rtt-sample-expiry` | `CHORD_RTT_SAMPLE_EXPIRY`¹ | `300s` | RTT sample expiry duration |
| `-piggyback-enabled` | `CHORD_PIGGYBACK_ENABLED` | `true` | Attach topology and RTT hints to responses |
| `-stabilize-debounce-threshold` | `CHORD_STABILIZE_DEBOUNCE` | `3` | Consecutive stabilize changes before debounce |
| `-topology-change-window` | `CHORD_TOPOLOGY_CHANGE_WINDOW`¹ | `120s` | Quiet period before switching to quiet maintenance mode |
| `-stabilize-active-interval` | `CHORD_STABILIZE_ACTIVE_INTERVAL`¹ | `15s` | Stabilize interval in active mode |
| `-stabilize-quiet-interval` | `CHORD_STABILIZE_QUIET_INTERVAL`¹ | `60s` | Stabilize interval in quiet mode |
| `-fix-fingers-active-interval` | `CHORD_FIX_FINGERS_ACTIVE_INTERVAL`¹ | `10s` | `fix_fingers` interval in active mode |
| `-fix-fingers-quiet-interval` | `CHORD_FIX_FINGERS_QUIET_INTERVAL`¹ | `30s` | `fix_fingers` interval in quiet mode |
| `-check-predecessor-active-interval` | `CHORD_CHECK_PREDECESSOR_ACTIVE_INTERVAL`¹ | `10s` | Predecessor health-check interval in active mode |
| `-check-predecessor-quiet-interval` | `CHORD_CHECK_PREDECESSOR_QUIET_INTERVAL`¹ | `30s` | Predecessor health-check interval in quiet mode |

### Bootstrap Behaviour

1. **With a tracker** (`-tracker-url`): the node fetches up to `tracker-seed-count` seeds and calls `POST /chord/join` on the first responsive one.
2. **With manual seeds** (`-seeds`): same process, using the provided URIs instead.
3. **No reachable seeds**: the node forms a single-node ring and waits for others to join.

The tracker is fully optional. Once the ring is running, removing the tracker has no effect on routing.

For v5.0 correctness experiments, initialize at least `r+1` long-lived physical anchor nodes before relying on the ring. VNodes improve load distribution but do not count toward the stable-base requirement.

## Node Lifecycle

```
INITIALIZING → JOINING → ACTIVE ⇄ LEAVING → (exit)
                              ↓         ↑
                           ISOLATED ────┘
```

| Status | Routing requests | Notes |
|---|---|---|
| `INITIALIZING` | No | Computing node ID, setting up data structures |
| `JOINING` | No | Executing join; waiting for successor assignment |
| `ACTIVE` | Yes | Normal operation |
| `LEAVING` | No (503) | Notifying neighbours, deregistering from tracker |
| `ISOLATED` | No (503) | All successors unreachable; trying to rejoin via tracker |

`GET /chord/ping` and `GET /chord/identity` always respond, including in `ISOLATED` state.

## HTTP API

All endpoints use `Content-Type: application/json`. Unknown fields in request bodies are rejected (`DisallowUnknownFields`).

### Node API

| Method | Path | Auth required | Description |
|---|---|---|---|
| `GET` | `/chord/identity` | No | Node ID, URI, status, join time |
| `GET` | `/chord/state` | Yes | Atomic Chord state snapshot: predecessor, successor list, validity metadata, and all 160 finger entries |
| `GET` | `/chord/ping` | No | Liveness probe (must respond within 5 s) |
| `POST` | `/chord/find_successor` | Yes | Iterative lookup — returns `found: true` + successor, or `found: false` + next hop |
| `GET` | `/chord/predecessor` | Yes | Current predecessor (`null` if none) |
| `POST` | `/chord/rectify` | Yes | v5 predecessor correction; validates current predecessor liveness before rejecting candidates |
| `POST` | `/chord/notify` | Yes | Backwards-compatible alias for `/chord/rectify` when enabled |
| `GET` | `/chord/successor_list` | Yes | Backup successor list (defaults to r=5 entries) |
| `POST` | `/chord/join` | Yes | Bootstrap entry point for a joining node |
| `POST` | `/chord/leave` | Yes | Graceful-leave notification from a departing neighbour |
| `GET` | `/chord/finger_table` | Yes | All 160 finger entries with repair status |
| `GET` | `/chord/invariant` | Yes | v5 successor-list invariant report and violation details |

`find_successor` uses **iterative** lookup: a node never chains HTTP calls internally. When the answer is not local it returns `{"found": false, "next_hop": {...}}` and the **caller** makes the next request. This keeps HTTP call depth at 1 regardless of ring size.

The "Auth required" column applies only when `--auth.enabled` is set. With auth disabled (default) all endpoints are open.

### Error Responses

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "human-readable description",
    "detail": {}
  }
}
```

| Status | Code | Trigger |
|---|---|---|
| 400 | `INVALID_REQUEST` | Malformed field (wrong `node_id` length, missing required field, unknown field) |
| 404 | `NODE_NOT_FOUND` | Tracker lookup for unknown node ID |
| 409 | `ID_COLLISION` | Joining node has same ID as an existing node |
| 401 | `MISSING_AUTH_HEADERS` | Required `X-Chord-*` headers absent |
| 401 | `TIMESTAMP_OUT_OF_WINDOW` | Request timestamp outside ±5-minute tolerance |
| 401 | `NONCE_REUSED` | Nonce has already been seen (replay attempt) |
| 401 | `CERTIFICATE_REQUIRED` | No cached cert for sender; retry with `X-Chord-Certificate` header |
| 401 | `INVALID_CERTIFICATE` | CA signature check failed, cert expired, or URI mismatch |
| 401 | `CERTIFICATE_REVOKED` | Sender's node_id appears in the CRL |
| 401 | `INVALID_SIGNATURE` | Ed25519 request signature does not verify |
| 503 | `NODE_ISOLATED` | `find_successor` received while isolated |
| 503 | `NODE_JOINING` | Rectify received while a node is still joining |
| 503 | `MAX_HOPS_EXCEEDED` | `hop_count` reached 161 |
| 503 | `NODE_LEAVING` | Any non-ping request while leaving |

### Tracker API (external)

This repo does **not** implement a tracker server — see [ChordDHT-Tracker](https://github.com/Rexezuge-CloudflareWorkers/ChordDHT-Tracker). The node client calls these endpoints if `-tracker-url` is configured:

| Method | Path | Description |
|---|---|---|
| `POST` | `/tracker/nodes` | Register on join (includes certificate when auth is enabled) |
| `DELETE` | `/tracker/nodes/{node_id}` | Deregister on graceful leave |
| `GET` | `/tracker/nodes/seeds?count=N&exclude=...&include_cert=true` | Fetch bootstrap seeds |
| `POST` | `/tracker/nodes/{node_id}/heartbeat` | Periodic heartbeat with ring statistics |
| `GET` | `/tracker/crl` | Fetch latest CRL (polled each maintenance cycle when auth enabled) |
| `GET` | `/tracker/stable_base` | On-demand stable-base liveness report for configured anchor URIs |

## NodeInfo Schema

```json
{
  "node_id":    "a94a8fe5ccb19ba61c4c0873d391e987982fbbd3",
  "uri":        "https://node1.example.com",
  "status":     "ACTIVE",
  "joined_at":  "2026-05-27T12:00:00Z",
  "certificate": { "...": "present in join/notify bodies when auth is enabled" }
}
```

`node_id` is always `sha1(normalized_uri)` as a 40-character lowercase hex string. Use `chord.NewNodeInfoFromURI` in tests rather than writing IDs by hand.

URI normalization rules: lowercase scheme and host, strip trailing slash, keep non-443 ports.

## Authentication (v2.0)

Node identity authentication is **opt-in** via `--auth.enabled`. When enabled, all node-to-node API calls (except `/chord/ping` and `/chord/identity`) require a valid Ed25519 request signature from a CA-issued certificate.

### Scheme Overview

- **You are the CA.** Your Ed25519 private key is the root of trust. Keep it offline.
- Each node holds its own Ed25519 key pair and a CA-signed **certificate** (custom JSON, not X.509).
- Every authenticated request carries four headers: `X-Chord-Node-ID`, `X-Chord-Timestamp`, `X-Chord-Nonce`, `X-Chord-Signature`.
- The first request to a peer also sends `X-Chord-Certificate`; subsequent requests use the receiver's cert cache (1-hour TTL).
- A **nonce cache** (10-minute TTL) prevents replay attacks.
- An optional **CRL** (CA-signed JSON) allows certificate revocation; the node refreshes it from the tracker each maintenance cycle.

### CA Tool

```sh
# 1. Generate CA key pair (one-time; keep CA_PRIVATE_KEY_BASE64 offline)
go run ./tools/ca gen-ca

# 2. Issue a certificate for each node
go run ./tools/ca issue \
  --ca-key=<CA_PRIVATE_KEY_BASE64> \
  --uri=https://node1.example.com \
  --days=365 \
  --out-dir=./creds
# Writes: <node_id>.cert.json  (distribute to node)
#         <node_id>.privkey.b64 (distribute to node, keep secret)

# 3. Generate / update a CRL
go run ./tools/ca gen-crl \
  --ca-key=<CA_PRIVATE_KEY_BASE64> \
  --revoke=<node_id1>,<node_id2> \
  --out=crl.json
```

### Authentication Flags

| Flag | Env var | Default | Description |
|---|---|---|---|
| `-auth.enabled` | `CHORD_AUTH_ENABLED` | `false` | Enable v2.0 identity authentication |
| `-auth.ca-public-key-base64` | `CHORD_AUTH_CA_PUBLIC_KEY_BASE64` | *(required if enabled)* | CA Ed25519 public key (base64url, 32 bytes) |
| `-auth.node-certificate-file` | `CHORD_AUTH_NODE_CERT_FILE` | *(required if enabled)* | Path to node certificate JSON file |
| `-auth.node-private-key-file` | `CHORD_AUTH_NODE_PRIVATE_KEY_FILE` | *(required if enabled)* | Path to node Ed25519 private key file (base64url, 64 bytes) |
| `-auth.timestamp-tolerance-secs` | `CHORD_AUTH_TIMESTAMP_TOLERANCE` | `300` | Request timestamp tolerance (±seconds) |
| `-auth.nonce-cache-ttl-secs` | `CHORD_AUTH_NONCE_CACHE_TTL` | `600` | Nonce cache TTL (seconds) |
| `-auth.nonce-cache-max-size` | `CHORD_AUTH_NONCE_CACHE_MAX_SIZE` | `10000` | Max cached nonces; rejects all when full |
| `-auth.cert-cache-ttl-secs` | `CHORD_AUTH_CERT_CACHE_TTL` | `3600` | Verified peer cert cache TTL (seconds) |
| `-auth.crl-file` | `CHORD_AUTH_CRL_FILE` | *(none)* | Local CRL JSON file path (optional) |
| `-auth.crl-refresh-from-tracker` | `CHORD_AUTH_CRL_REFRESH` | `true` | Poll tracker's `GET /tracker/crl` each maintenance cycle |
| `-auth.cert-expiry-warn-days` | `CHORD_AUTH_CERT_EXPIRY_WARN` | `30` | Log WARN when cert expires within this many days |
| `-auth.boot-grace-period-secs` | `CHORD_AUTH_BOOT_GRACE` | `0` | Seconds after startup before auth is enforced (mitigates nonce-cache restart window) |

### Example: Starting a Node with Auth

```sh
node \
  -uri          https://node1.example.com \
  -tls-cert     /certs/tls.crt \
  -tls-key      /certs/tls.key \
  -tracker-url  https://tracker.example.com \
  -auth.enabled \
  -auth.ca-public-key-base64   <CA_PUBLIC_KEY_BASE64> \
  -auth.node-certificate-file  /creds/<node_id>.cert.json \
  -auth.node-private-key-file  /creds/<node_id>.privkey.b64
```

### Certificate Format

Certificates are a custom lightweight JSON format (not X.509), signed by the CA over a canonical plaintext message:

```json
{
  "version":    1,
  "node_id":    "a94a8fe5ccb19ba61c4c0873d391e987982fbbd3",
  "uri":        "https://node1.example.com",
  "public_key": "<base64url 32-byte Ed25519 public key>",
  "issued_at":  1748390400,
  "expires_at": 1779926400,
  "signature":  "<base64url 64-byte CA Ed25519 signature>"
}
```

`node_id` must equal `SHA1(normalized_uri)` — certificates cannot be transferred between nodes.

## Development

```sh
# Run all tests
go test ./...

# Run one package
go test ./internal/chord

# Run one test
go test ./internal/client -run TestJSONClientCanSkipTLSVerification

# Format changed files
gofmt -w <files>

# Build node binary
go build ./cmd/node

# Build CA tool
go build ./tools/ca
```

CI runs `go test ./...` on every push and pull request to `main`. Releases publish static binaries for `linux/amd64` and `linux/arm64`.

## Further Reading

- Stoica et al., *"Chord: A Scalable Peer-to-peer Lookup Service for Internet Applications"*, ACM SIGCOMM 2001
- Maymounkov & Mazières, *"Kademlia: A Peer-to-peer Information System Based on the XOR Metric"*, IPTPS 2002 — a complementary DHT design used in BitTorrent and IPFS
