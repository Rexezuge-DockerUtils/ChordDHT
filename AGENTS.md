# AGENTS.md

## Repo Shape
- This is a small Go module (`module chorddht`, Go 1.26) with no external dependencies or Makefile. The README documents the current v5.0 protocol surface.
- `cmd/node` is the only executable. It starts an HTTPS Chord DHT node and wires `internal/config`, `internal/chord`, `internal/client`, `internal/httpapi`, and `internal/logging`.
- `internal/chord` owns ring state, lookup, join/leave, stabilization, successor lists, finger repair, and tracker reporting. `internal/httpapi` is only the JSON HTTP wrapper around `chord.Node` methods.
- **v4.0 vnode architecture**: `internal/chord/vnode_proof.go` contains `VNodeProof`, `DeriveVNodeID`, `SignVNodeProof`, `VerifyVNodeProof`. `internal/chord/shared_resources.go` contains `NodeInfoCache` and `ProofVerifyCache` (L0 shared resources). `httpapi.NodePool` wraps the anchor + vnodes and routes `/chord/node/{id}/*` paths. `PeerClient` methods that need vnode-specific routing take `NodeInfo` (not just `string`): `FindSuccessor`, `Notify`, `Predecessor`, `SuccessorList`, `Leave`.
- **v5.0 Zave corrections**: `HandleRectify` replaces Notify semantics while `/notify` remains a configurable alias. Stabilize should use `PeerClient.State` to get atomic predecessor+successor-list snapshots, ping candidate predecessors with `PingLiveness`, then call `Rectify`. Join must initialize successor lists as `[successor] + successor.successor_list[:r-1]`. `validateSuccessorList` maintains `successor_list_valid` and `last_invariant_check`; `/chord/invariant` exposes a debug report.

## Commands
- Run all tests: `go test ./...`
- Run one package: `go test ./internal/chord`
- Run one test: `go test ./internal/client -run TestJSONClientCanSkipTLSVerification`
- Build the node binary: `go build ./cmd/node`
- Format changed Go files before verification: `gofmt -w <files>`

## Runtime Gotchas
- The node is strict HTTPS only. `config.Load` rejects non-HTTPS `-uri`/`NODE_URI` and requires `-tls-cert`/`NODE_TLS_CERT_FILE` plus `-tls-key`/`NODE_TLS_KEY_FILE`; `main` calls `ListenAndServeTLS`.
- Peer and tracker URIs must be normalized absolute HTTPS URIs with no userinfo, query, fragment, or path. Port `443` is stripped, host is lowercased, and node IDs are `sha1(normalizedURI)`.
- CLI duration flags use Go duration syntax (for example `-http-timeout=5s`); env duration values are integer seconds (`CHORD_HTTP_TIMEOUT_SECONDS`, `CHORD_MAINTENANCE_INTERVAL_SECONDS`).
- `TRACKER_URL` is optional. If no tracker or manual seeds are usable, `JoinNetwork` activates a single-node ring instead of failing.
- `CHORD_SKIP_TLS_VERIFY=true` is only for outbound peer/tracker clients; the local server still requires cert/key files.
- v5 stable-base settings are operational preconditions, not routing logic. Configure physical anchor URIs with `CHORD_STABLE_BASE_MEMBERS`; VNodes do not count toward `CHORD_STABLE_BASE_MIN_SIZE`.

## API/Protocol Notes
- JSON request handlers call `DisallowUnknownFields`, so tests/clients should not send extra fields.
- Main Chord endpoints are under `/chord/*` (legacy, routes to anchor) and `/chord/node/{node_id}/*` (v4.0, routes to specific vnode or anchor by ID).
- `NodeInfo` validation requires a 40-character lowercase hex `node_id` matching `sha1(uri)` for anchors. For vnodes (`anchor_id != ""`), the ID check is skipped — vnode IDs are derived, not URI-based.
- Tracker endpoints are external assumptions only. This repo does not implement a tracker server.
- v5 endpoints: `POST /chord/rectify`, `GET /chord/invariant`, and extended `GET /chord/state` fields (`successor_list_valid`, `last_invariant_check`, `is_stable_base_member`, `snapshot_timestamp`).
- The `PeerClient` interface uses `NodeInfo` (not URI strings) for `FindSuccessor`, `Notify`, `Rectify`, `State`, `Predecessor`, `SuccessorList`, `Leave` so the client can automatically route to `/chord/node/{id}/` when `target.AnchorID != ""`.
- Test mocks that implement `PeerClient` must provide all methods: `Ping`, `PingLiveness`, `PingWithLatency`, `FindSuccessor`, `Join`, `Notify`, `Rectify`, `State`, `Predecessor`, `SuccessorList`, `Leave`, and `RTT`.

## Git Commit Messages

- Use Conventional Commits with this subject format: `<TYPE>[optional scope]: <description>`.
- Write the type in uppercase, for example `FIX`, `FEAT`, `DOCS`, `STYLE`, `REFACTOR`, `TEST`, `BUILD`, `CHORE`, `CI`, or `PERF`.
- Write the optional scope in lowercase inside parentheses, for example `FEAT(runtime): Add Scheduled Job State`.
- Write the description as concise human-readable words with spaces, capitalizing the first letter of each word, for example `DOCS: Latest Agents Context Reflection`, `STYLE: Standardize Python Formatting`, or `FEAT: Bootstrap JQAnywhere v0.1 Framework`.
- When creating a commit from `main`, first switch to a new branch generated from the planned commit subject.
- Use lowercase slash-separated branch names: `type/description` when there is no scope, or `type/scope/description` when there is a scope.
- Convert the description to kebab-case for the branch, for example `docs/latest-agents-context-reflection`, `docs/agents/document-commit-standard`, or `feat/bootstrap/bootstrap-jqanywhere-v0.1-framework`.
- Use Markdown for optional commit bodies, separated from the subject by a blank line.
- Use optional footers after the body, separated by a blank line, following git trailer-style formatting.
- Use `FIX` for bug patches and `FEAT` for new features; other conventional types are allowed when they better communicate intent.
- Mark breaking API changes with `!` after the type or scope, or with a `BREAKING CHANGE: <description>` footer.

```text
<TYPE>[optional scope]: <description>

[optional body in Markdown]

[optional footer(s)]
```
