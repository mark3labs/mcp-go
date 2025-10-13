<!-- OPENSPEC:START -->
# OpenSpec Instructions

These instructions are for AI assistants working in this project.

Always open `@/openspec/AGENTS.md` when the request:
- Mentions planning or proposals (words like proposal, spec, change, plan)
- Introduces new capabilities, breaking changes, architecture shifts, or big performance/security work
- Sounds ambiguous and you need the authoritative spec before coding

Use `@/openspec/AGENTS.md` to learn:
- How to create and apply change proposals
- Spec format and conventions
- Project structure and guidelines

Keep this managed block so 'openspec update' can refresh the instructions.

<!-- OPENSPEC:END -->

# Commands
- **Test all**: `go test ./... -race`
- **Test package**: `go test ./mcp -v` or `go test ./server -v`
- **Test single**: `go test -run TestName ./package -v`
- **Coverage**: `go test -coverprofile=coverage.txt -covermode=atomic $(go list ./... | grep -v '/examples/' | grep -v '/testdata' | grep -v '/mcptest' | grep -v '/server/internal/gen')`
- **Lint**: `golangci-lint run` (uses .golangci.yml config)
- **Generate**: `go generate ./...` (regenerates hooks and request handlers)

# Code Style
- **Imports**: Standard library first, then third-party, then local packages (goimports handles this)
- **Naming**: Use Go conventions - exported names (PascalCase), unexported names (camelCase), acronyms uppercase (HTTP, JSON, MCP)
- **Error handling**: Return sentinel errors (e.g., `ErrMethodNotFound`), wrap with `fmt.Errorf("context: %w", err)`, use `errors.Is/As` for checking
- **Types**: Use explicit types; avoid `any` except for protocol flexibility (e.g., `Arguments any`); prefer strongly-typed structs
- **Comments**: All exported types/functions MUST have godoc comments starting with the name; no inline comments unless necessary
- **Testing**: Use `testify/assert` and `testify/require`; table-driven tests with `tests := []struct{ name, ... }`; test files end in `_test.go`
- **Context**: Always accept `context.Context` as first parameter in handlers and long-running functions
- **Thread safety**: Use `sync.Mutex` for shared state; document thread-safety requirements in comments
- **JSON**: Use json tags with `omitempty` for optional fields; use `json.RawMessage` for flexible/deferred parsing
