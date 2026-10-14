---
description: Read-only audit for dead code, duplication, boundary violations, and refactor opportunities
---

Perform a comprehensive **read-only** audit of this repository (the
`github.com/mark3labs/mcp-go` SDK) and report findings. **Do not edit,
rename, or delete any files.** Optional focus / scope hints from the user:
$@

## Scope

If the user supplied focus hints above (a package path like `server/`, a
subsystem name like `streamable_http`, `transport`, `oauth`, `hooks`,
`schema_cache`), scope the audit accordingly. Otherwise audit the whole
module, prioritising the highest-traffic packages first in this order:

1. `server/` — MCP server, hooks, sessions, transports
2. `client/` and `client/transport/` — MCP client and pluggable transports
3. `mcp/` — protocol types, schemas, tools, prompts, resources

Lower-priority surfaces: `mcptest/`, `util/`, `e2e/`.

**Always skip** the following from active findings:
- `examples/**` (reference code, excluded from coverage)
- `testdata/**` and any `*_test.go` fixtures used only by tests
- `server/internal/gen/**` (the code generator itself)
- The two generated files `server/hooks.go` and
  `server/request_handler.go` — these are produced from
  `server/internal/gen/*.tmpl` by `go generate ./...` and the
  `Code generated … DO NOT EDIT.` header is the giveaway. Refactor
  proposals against these files belong against the **templates**, never
  the generated output.
- `vendor/**` if present, and any obvious third-party copies

## Steps

1. **Map the repo first**:
   - `ls` the module root and enumerate every Go package under `mcp/`,
     `client/`, `client/transport/`, `server/`, `server/internal/`,
     `mcptest/`, `util/`, `e2e/`
   - Read `AGENTS.md`, `README.md`, `.golangci.yml`, and
     `server/internal/gen/README.md` to confirm the documented
     conventions (sentinel errors, `%w` wrapping, godoc on every exported
     symbol, `omitempty` on optional JSON fields, `context.Context` first
     arg, `sync.Mutex` for shared state, `json.RawMessage` for deferred
     parsing, generated files from `go generate`)
   - Note the **public SDK surface**: everything outside `internal/` is
     importable by downstream users. Breaking changes to `mcp/`,
     `client/`, `client/transport/`, `server/`, or `mcptest/` need extra
     justification

2. **Hunt for dead code**:
   - Run `go vet ./...` and capture warnings
   - Run `go build ./...` to confirm the tree compiles before drawing
     conclusions
   - Use `grep` to find exported symbols (`^func [A-Z]`, `^type [A-Z]`,
     `^var [A-Z]`, `^const [A-Z]`) and cross-reference call sites within
     the module **and** within `examples/` (examples are a real user of
     the SDK — an exported symbol used only by examples is not dead, but
     an exported symbol with zero references anywhere is a candidate)
   - Check unexported symbols whose only references are in the same file
     or in tests that exercise nothing else
   - Look for unreferenced files, `// TODO: remove` markers,
     commented-out blocks, and `_ = x` discard patterns
   - If `staticcheck`, `deadcode`, or `unused` are available on PATH, run
     them across `./...` and include their output verbatim. Filter out
     hits inside the skip list above.
   - **Do not delete anything** — list candidates with `file:line` and a
     confidence level (high / medium / low). For exported symbols, always
     mark confidence **medium at best** — downstream users may import
     them.

3. **Find unnecessary duplication**:
   - Search for near-identical function bodies, struct shapes, switch
     statements, and error-message strings across packages
   - **Critically distinguish symmetric protocol duplication from real
     duplication.** The transports (`stdio`, `sse`, `streamable_http`,
     `inprocess`) appear on *both* sides — `client/transport/` and
     `server/` — because they implement opposite ends of the same wire
     protocol. Shape similarity there is expected and should *not* be
     flagged unless the duplication is **inside the same side** (e.g.
     two server transports that re-implement the same JSON-RPC framing
     helper) or genuinely belongs in `mcp/` as a shared protocol helper.
   - Likewise, mirrored client/server type pairs (request/response,
     elicitation, sampling, roots) often need to stay independent to let
     each side evolve its validation rules — only flag if both copies
     have drifted in lockstep across multiple commits.
   - For each genuine cluster, propose where the extracted helper should
     live:
     - Pure protocol/JSON helpers → `mcp/`
     - Client-only helpers → `client/` or `client/transport/`
     - Server-only helpers → `server/` (or a new `server/internal/...`
       subpackage if it should not be exported)
     - Cross-cutting infra (logging, etc.) → `util/`

4. **Check boundary violations**:
   - **Protocol layer purity**: `mcp/` must not import `client/`,
     `client/transport/`, `server/`, `mcptest/`, or `util/`. Grep
     `mcp/*.go` for those import paths.
   - **Client ↔ server isolation**: `client/...` must not import
     `server/...` and vice versa — they are peers built on `mcp/`. A
     shared need belongs in `mcp/` or a new shared package.
   - **Transport layering**: `client/transport/` should not import the
     high-level `client/` package (would create a cycle); it consumes
     `mcp/` for types and exposes `transport.Interface` upward.
   - **`internal/` visibility**: `server/internal/...` may only be
     imported by `server/...`. Grep all packages for
     `mark3labs/mcp-go/server/internal/` and flag anything outside
     `server/`.
   - **`mcptest/` direction**: production code in `mcp/`, `client/`,
     `client/transport/`, `server/`, `util/` must not import `mcptest/`
     — that package depends on them, not the other way around.
   - **`examples/` direction**: nothing in the library should import
     anything under `examples/...`.
   - **Generated-file edits**: if `server/hooks.go` or
     `server/request_handler.go` contain hand-written drift relative to
     their templates (`server/internal/gen/*.tmpl`), call that out as a
     boundary violation — the source of truth is the template.
   - **Cyclic risk**: packages that import each other transitively, or
     that reach across sibling boundaries unexpectedly. `go list -f
     '{{.ImportPath}} -> {{.Imports}}' ./...` is a useful read-only
     probe.
   - For each violation, cite the offending import / signature with
     `file:line`.

5. **Spot refactor opportunities**:
   - Long functions (>80 lines) in `server/streamable_http.go`,
     `server/streamable_http_handle.go`, `client/transport/streamable_http.go`,
     `server/sse.go`, `client/transport/sse.go`, and the OAuth flows are
     prime candidates — but check they aren't already a transcription of
     the spec where the linearity *is* the readability win.
   - Deeply nested conditionals that flatten with early returns
   - Repeated `if err != nil { return fmt.Errorf("...: %w", err) }`
     chains — only flag where the wrapping context is genuinely uniform
     and a helper would not obscure which call site failed (sentinel
     errors like `ErrMethodNotFound` from `mcp/errors.go` should keep
     their explicit `errors.Is/As` checks)
   - Structs with too many fields that hint at split responsibilities
     (the `MCPServer`, `Client`, and streamable HTTP session structs are
     worth scanning, but be aware they intentionally aggregate
     protocol-required state)
   - Exported APIs that would be cleaner with options structs / the
     functional-options pattern already used elsewhere in the SDK
     (`server.WithToolCapabilities`, `client.WithHTTPClient`, etc.) —
     adding new options is backward-compatible; renaming existing ones
     is not
   - Tests that share setup boilerplate ripe for a helper in `mcptest/`
     or a `_test.go` file-local helper
   - JSON handling: missed `omitempty` on optional fields, hand-rolled
     marshalling where a struct tag would do, `interface{}`/`any` where
     a concrete type is already known
   - Thread-safety: shared state without a `sync.Mutex`, or a `Mutex`
     held across a network call
   - Flag each with: location, current shape (1-2 lines), proposed shape
     (1-2 lines), and estimated risk (low / medium / high). For anything
     touching exported types in `mcp/`, `client/`, `client/transport/`,
     `server/`, or `mcptest/`, default the risk to **medium** because of
     SDK compatibility.

6. **Spec / schema parity check** (latest MCP schema):
   - The upstream protocol schemas live at
     <https://github.com/modelcontextprotocol/modelcontextprotocol/tree/main/schema>
     in dated subdirectories (e.g. `2025-06-18/`, `2025-11-25/`,
     `draft/`). Each version ships both a `schema.ts` (TypeScript source
     of truth) and a generated `schema.json` (JSON Schema).
   - Identify what *this* repo claims to support:
     - Read `mcp/types.go` for `LATEST_PROTOCOL_VERSION` and
       `ValidProtocolVersions` (currently anchored on `2025-11-25`)
     - Note the spec-URL godoc references throughout `mcp/types.go`
       (`modelcontextprotocol.io/specification/<date>/...`) — each one
       pins a feature to a specific dated revision
     - Skim open issues / PRs (`gh issue list`, `gh pr list`) for
       in-flight compliance work already acknowledged by maintainers —
       items already filed there are **not** new findings, but they
       are useful to confirm whether a gap is known
   - Fetch the latest schema(s) read-only. Prefer one of:
     - `gh api repos/modelcontextprotocol/modelcontextprotocol/contents/schema/<version>/schema.json --jq .content | base64 -d > /tmp/mcp-schema-<version>.json`
     - `curl -sSL https://raw.githubusercontent.com/modelcontextprotocol/modelcontextprotocol/main/schema/<version>/schema.json -o /tmp/mcp-schema-<version>.json`
     - Or `git clone --depth 1 https://github.com/modelcontextprotocol/modelcontextprotocol /tmp/mcp-spec` if network policy allows
     Fetch at minimum the version named by `LATEST_PROTOCOL_VERSION`,
     and additionally `draft/` so you can flag features that are
     stabilising upstream but not yet implemented here.
   - Diff the schema against this SDK:
     - **Methods / RPC requests**: every `*Request` / `*Result` pair in
       the schema should have a corresponding Go type in `mcp/`. Grep
       `mcp/consts.go` and `mcp/types.go` for the method name string
       (e.g. `"tasks/create"`, `"completion/complete"`). Missing
       constants or types are candidate gaps.
     - **Notifications**: schema `*Notification` definitions should
       have matching constants in `mcp/consts.go` and handler hooks in
       `server/` (handlers are template-generated, so a gap shows up
       as a missing template entry under
       `server/internal/gen/data.go`).
     - **Capabilities**: `ClientCapabilities` / `ServerCapabilities`
       fields in the schema vs. the Go structs in `mcp/types.go`.
       Missing fields, missing nested capability shapes, or a Go field
       lacking `omitempty` where the schema marks it optional are all
       valid findings.
     - **Content types**: any new content block variants
       (`text`, `image`, `audio`, `resource`, `resource_link`,
       newer additions) should be present in `mcp/types.go` and the
       relevant helper constructors (`NewToolResult*`,
       `NewTextContent`, etc.).
     - **Enum / role / kind values**: schema string-enum changes (new
       log levels, new role values, new task statuses, new icon kinds)
       vs. the constant blocks in `mcp/consts.go` and `mcp/types.go`.
     - **Field-level drift**: existing types where the schema added,
       removed, or renamed a field — including `description`,
       `title`, `_meta`, `annotations`, `cursor` pagination, etc.
   - For each gap, record:
     - Schema location: `schema/<version>/schema.ts:LINE` or the JSON
       `$defs/<Name>` path
     - SDK location of the *closest existing* type: `mcp/types.go:LINE`
       (or `"(not implemented)"`)
     - Whether an open issue / PR already tracks it (link it)
     - Confidence: **high** if the schema field is `required`, **medium**
       if optional, **low** for `draft/`-only features
   - Do **not** propose code changes here — only enumerate the
     compliance gaps. The user can triage them into individual GitHub
     issues or `/fix-issue` runs.

7. **Cross-check against project rules**:
   - Re-read `AGENTS.md` and verify nothing in your findings contradicts
     the documented conventions:
     - exported symbols must keep their godoc comments
     - sentinel errors stay sentinels (`ErrMethodNotFound` etc.)
     - `context.Context` is always the first parameter in handlers and
       long-running functions
     - `omitempty` on optional JSON fields, `json.RawMessage` for
       deferred parsing
     - tests use `testify/assert` + `testify/require` and table-driven
       layout
     - generated files come from `go generate ./...`; never propose
       hand-editing `server/hooks.go` or `server/request_handler.go`
   - If a proposed "refactor" would break any of those rules, drop it
     from the report and briefly note why.

8. **Write the report** as your final message (do not write it to disk)
   structured as:

   ```
   # Code Audit Report

   ## Summary
   - N dead-code candidates
   - N duplication clusters
   - N boundary violations
   - N refactor opportunities
   - N spec-parity gaps (against schema <version>; current
     LATEST_PROTOCOL_VERSION is <value>)

   ## Dead Code
   ### High confidence
   - path/to/file.go:LINE — symbol — reason

   ### Medium confidence
   ...

   ## Duplication
   ### Cluster: <short name>
   - Sites: file:line, file:line, …
   - Suggested home: package/path (e.g. `mcp/`, `server/`, new internal helper)
   - Notes: why this is *unnecessary* duplication, not the symmetric
     client/server protocol mirroring

   ## Boundary Violations
   - Rule: <which rule from AGENTS.md / project convention>
   - Offender: file:line
   - Fix sketch: …

   ## Refactor Opportunities
   - Location: file:line
   - Current: …
   - Proposed: …
   - Risk: low/medium/high (default medium for any exported SDK surface)
   - Why it's worth it: …

   ## Spec Parity Gaps
   - Schema: schema/<version>/schema.ts:LINE (or $defs/<Name>)
   - SDK: mcp/types.go:LINE (or "(not implemented)")
   - Required upstream? yes/no — Already tracked in an issue/PR? yes/no + link
   - Confidence: high/medium/low (`draft/`-only features default to low)
   - Notes: …

   ## Suggested Next Steps
   1. …
   2. …
   ```

9. **End the report with an explicit reminder** that no files were
   modified, that `server/hooks.go` and `server/request_handler.go` are
   generated and must be changed via the templates in
   `server/internal/gen/` followed by `go generate ./...`, that
   spec-parity gaps should be cross-checked against open issues / PRs
   (and likely warrant new GitHub issues rather than a single sweeping
   PR), and that anything touching the exported
   SDK surface should be reviewed for backward compatibility before
   action. Recommend the user pick the highest-leverage items to act
   on manually (or via a follow-up `/fix-issue` style prompt) rather
   than running a sweeping refactor.

## Guidelines

- **Read-only, always**: no `edit`, no `write`, no `git commit`, no
  `go mod tidy`, no `go generate`. Use only `read`, `grep`, `find`, `ls`,
  and read-only `bash` commands (`go vet ./...`, `go build ./...`,
  `go list ./...`, `staticcheck`, `golangci-lint run` is read-only when
  not paired with `--fix`). Fetching the upstream MCP schema with
  `curl`, `gh api`, or a shallow `git clone` into `/tmp/` is fine — it
  does not modify this repo.
- **Cite every finding** with `path/to/file.go:LINE` so the user can
  jump straight to it.
- **Be honest about confidence**: false positives in a code audit are
  expensive — prefer "medium confidence, worth a look" over confidently
  wrong claims. For exported SDK symbols, downstream users you cannot
  see may depend on them; cap confidence at medium.
- **Quantity isn't quality**: 10 sharp findings beat 100 nitpicks. Cut
  anything purely stylistic unless it directly causes one of the four
  issue categories above.
- **Respect the symmetric duplication**: client-side and server-side
  transport implementations are intentionally parallel; don't propose
  merging them into a single package.
- **Skip generated and example code** (`server/hooks.go`,
  `server/request_handler.go`, `examples/**`, `testdata/**`,
  `server/internal/gen/**`).
- **Don't propose architectural rewrites** — stay within the existing
  shape of the module (`mcp/` + `client/` + `server/` + transports) and
  recommend incremental, reviewable changes.
