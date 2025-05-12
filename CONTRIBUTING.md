# Contributing

Thank you for your interest in contributing to the MCP Go SDK! We welcome contributions of all kinds, including bug fixes, new features, and documentation improvements. This document outlines the process for contributing to the project.

## Development Guidelines

### Prerequisites

Make sure you have Go 1.23 or later installed on your machine. You can check your Go version by running:

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
5. Submit a pull request to the main branch.

Feel free to reach out if you have any questions or need help either by [opening an issue](https://github.com/mark3labs/mcp-go/issues) or by reaching out in the [Discord channel](https://discord.gg/RqSS2NQVsY).
