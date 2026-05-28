# ChordDHT

A learning implementation of the [Chord](https://pdos.csail.mit.edu/papers/ton:chord/paper-ton.pdf) distributed hash table protocol (Stoica et al., ACM SIGCOMM 2001), written in Go with no external dependencies.

> **Scope:** Chord ring formation and O(log N) key routing only. No key-value storage. Not intended for production use.

## Sister Project

[**ChordDHT-Tracker**](https://github.com/Rexezuge-CloudflareWorkers/ChordDHT-Tracker) — the optional bootstrap tracker for this node, implemented as a Cloudflare Worker with a D1 database and a React ring-visualization dashboard.

## How It Works

Every node maps its canonical HTTPS URI to a 160-bit ID via SHA-1 and joins a consistent-hash ring. Routing uses an iterative finger-table lookup that resolves any key in at most O(log N) hops. A 60-second maintenance cycle runs `check_predecessor → stabilize → fix_fingers → health_check_ring → report_to_tracker` to keep the ring self-healing.

Key protocol parameters:

| Parameter | Value | Notes |
|---|---|---|
| ID space | 2¹⁶⁰ (SHA-1) | Matches the original Chord paper |
| Finger table | 160 entries | One entry repaired per maintenance cycle |
| Successor list | r = 3 | Tolerates up to 3 consecutive node failures |
| Maintenance interval | 60 s | Configurable |
| Lookup mode | Iterative | HTTP call depth is always 1; caller drives hops |

## Project Layout

```
cmd/node/           # Executable — starts an HTTPS Chord node
internal/chord/     # Ring state, lookup, join/leave, stabilization, finger repair
internal/httpapi/   # JSON HTTP wrapper around chord.Node
internal/client/    # Outbound HTTP clients for peer and tracker
internal/config/    # CLI flags + environment variable loading
internal/logging/   # Levelled logger
```

## Building

```sh
go build ./cmd/node
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
| `-successor-list-size` | `CHORD_SUCCESSOR_LIST_SIZE` | `3` | Successor list length (r) |
| `-max-hops` | `CHORD_MAX_HOPS` | `161` | Hard hop limit for `find_successor` |
| `-suspicious-threshold` | `CHORD_SUSPICIOUS_THRESHOLD` | `1` | Consecutive failures before marking a peer suspicious |
| `-failed-threshold` | `CHORD_FAILED_THRESHOLD` | `3` | Consecutive failures before evicting a peer from routing tables |
| `-tracker-seed-count` | `TRACKER_SEED_COUNT` | `5` | How many seed nodes to request from the tracker on join |

¹ Environment duration values are **integer seconds** (e.g. `CHORD_HTTP_TIMEOUT_SECONDS=10`). CLI flags use Go duration syntax (e.g. `-http-timeout=10s`).

### Bootstrap Behaviour

1. **With a tracker** (`-tracker-url`): the node fetches up to `tracker-seed-count` seeds and calls `POST /chord/join` on the first responsive one.
2. **With manual seeds** (`-seeds`): same process, using the provided URIs instead.
3. **No reachable seeds**: the node forms a single-node ring and waits for others to join.

The tracker is fully optional. Once the ring is running, removing the tracker has no effect on routing.

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

| Method | Path | Description |
|---|---|---|
| `GET` | `/chord/identity` | Node ID, URI, status, join time |
| `GET` | `/chord/state` | Full Chord state: predecessor, successor, successor list, all 160 finger entries |
| `GET` | `/chord/ping` | Liveness probe (must respond within 5 s) |
| `POST` | `/chord/find_successor` | Iterative lookup — returns `found: true` + successor, or `found: false` + next hop |
| `GET` | `/chord/predecessor` | Current predecessor (`null` if none) |
| `POST` | `/chord/notify` | Predecessor candidate announcement |
| `GET` | `/chord/successor_list` | Backup successor list (up to r=3 entries) |
| `POST` | `/chord/join` | Bootstrap entry point for a joining node |
| `POST` | `/chord/leave` | Graceful-leave notification from a departing neighbour |
| `GET` | `/chord/finger_table` | All 160 finger entries with repair status |

`find_successor` uses **iterative** lookup: a node never chains HTTP calls internally. When the answer is not local it returns `{"found": false, "next_hop": {...}}` and the **caller** makes the next request. This keeps HTTP call depth at 1 regardless of ring size.

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
| 503 | `NODE_ISOLATED` | `find_successor` received while isolated |
| 503 | `MAX_HOPS_EXCEEDED` | `hop_count` reached 161 |
| 503 | `NODE_LEAVING` | Any non-ping request while leaving |

### Tracker API (external)

This repo does **not** implement a tracker server — see [ChordDHT-Tracker](https://github.com/Rexezuge-CloudflareWorkers/ChordDHT-Tracker). The node client calls these endpoints if `-tracker-url` is configured:

| Method | Path | Description |
|---|---|---|
| `POST` | `/tracker/nodes` | Register on join |
| `DELETE` | `/tracker/nodes/{node_id}` | Deregister on graceful leave |
| `GET` | `/tracker/nodes/seeds?count=N&exclude=...` | Fetch bootstrap seeds |
| `POST` | `/tracker/nodes/{node_id}/heartbeat` | Periodic heartbeat with ring statistics |

## NodeInfo Schema

```json
{
  "node_id": "a94a8fe5ccb19ba61c4c0873d391e987982fbbd3",
  "uri":     "https://node1.example.com",
  "status":  "ACTIVE",
  "joined_at": "2026-05-27T12:00:00Z"
}
```

`node_id` is always `sha1(normalized_uri)` as a 40-character lowercase hex string. Use `chord.NewNodeInfoFromURI` in tests rather than writing IDs by hand.

URI normalization rules: lowercase scheme and host, strip trailing slash, keep non-443 ports.

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

# Build
go build ./cmd/node
```

CI runs `go test ./...` on every push and pull request to `main`. Releases publish static binaries for `linux/amd64` and `linux/arm64`.

## Further Reading

- Stoica et al., *"Chord: A Scalable Peer-to-peer Lookup Service for Internet Applications"*, ACM SIGCOMM 2001
- Maymounkov & Mazières, *"Kademlia: A Peer-to-peer Information System Based on the XOR Metric"*, IPTPS 2002 — a complementary DHT design used in BitTorrent and IPFS
