# Contributing

Thank you for your interest in contributing to the MCP Go SDK! We welcome contributions of all kinds, including bug fixes, new features, and documentation improvements. This document outlines the process for contributing to the project.

## Development Guidelines

### Prerequisites

Make sure you have Go 1.25 or later installed on your machine. You can check your Go version by running:

```bash
go version
```

### Setup

1. Fork the repository
2. Clone your fork:
   
   ```bash
    git clone https://github.com/YOUR_USERNAME/mcp-go.git
    cd mcp-go
    ```
3. Install the required packages:

    ```bash
    go mod tidy
    ```

### Workflow

1. Create a new branch.
2. Make your changes.
3. Ensure you have added tests for any new functionality.
4. Run the tests as shown below from the root directory:

    ```bash
    go test -v './...'
    ```

    The `otel` submodule has its own `go.mod`. To test it:

    ```bash
    cd otel && go test -v './...'
    ```
5. Submit a pull request to the main branch.

## Releasing

The repository contains two Go modules:

- `github.com/mark3labs/mcp-go/v2` (root) — core, no OpenTelemetry deps.
- `github.com/mark3labs/mcp-go/otel` — OpenTelemetry adapter, has its own
  `go.mod`.

Core releases use semantic `v2.X.Y` tags and the `/v2` module path. The
OpenTelemetry adapter is versioned independently with `otel/vX.Y.Z` tags.

Tag the core module first, then bump `otel/go.mod`'s `require` line to the new
core tag (replacing the `replace` directive used during development), then tag
the submodule:

```bash
# 1. Tag the core
git tag -a v2.X.Y -m "v2.X.Y - release summary"
git push origin refs/tags/v2.X.Y

# 2. Bump otel/go.mod and remove the replace directive
cd otel
go mod edit -require=github.com/mark3labs/mcp-go/v2@v2.X.Y -dropreplace=github.com/mark3labs/mcp-go/v2
go mod tidy

# 3. Commit and tag the submodule
git commit -am "otel: pin core to v2.X.Y"
git tag -a otel/vX.Y.Z -m "otel/vX.Y.Z - release summary"
git push origin refs/tags/otel/vX.Y.Z
```

Feel free to reach out if you have any questions or need help either by [opening an issue](https://github.com/mark3labs/mcp-go/issues) or by reaching out in the [Discord channel](https://discord.gg/RqSS2NQVsY).
