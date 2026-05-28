# AGENTS.md

## Repo Shape
- This is a small Go module (`module chorddht`, Go 1.26) with no external dependencies, Makefile, CI workflow, or useful README content.
- `cmd/node` is the only executable. It starts an HTTPS Chord DHT node and wires `internal/config`, `internal/chord`, `internal/client`, `internal/httpapi`, and `internal/logging`.
- `internal/chord` owns ring state, lookup, join/leave, stabilization, successor lists, finger repair, and tracker reporting. `internal/httpapi` is only the JSON HTTP wrapper around `chord.Node` methods.

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

## API/Protocol Notes
- JSON request handlers call `DisallowUnknownFields`, so tests/clients should not send extra fields.
- Main Chord endpoints are under `/chord/*`; tracker endpoints are external assumptions only (`/tracker/nodes`, `/tracker/nodes/seeds`, heartbeat/delete paths). This repo does not implement a tracker server.
- `NodeInfo` validation requires a 40-character lowercase hex `node_id` matching `sha1(uri)`; use `NewNodeInfoFromURI` in tests instead of hand-writing IDs.

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
