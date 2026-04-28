# Lore Context MCP Server

MCP server wrapping the [Lore Context](https://github.com/Lore-Context/lore-context) REST API as tools.

## Usage

```bash
# Stdio (default)
LORE_API_URL=http://localhost:3120 LORE_API_KEY=your-key go run .

# HTTP
go run . -transport http -addr :8080
```

### Claude Desktop config

```json
{
  "mcpServers": {
    "lore-context": {
      "command": "go",
      "args": ["run", "/path/to/examples/lore_context"],
      "env": {
        "LORE_API_URL": "http://localhost:3120",
        "LORE_API_KEY": "<YOUR_LORE_API_KEY>"
      }
    }
  }
}
```

## Tools

- `lore_memory_search` — search memories by query
- `lore_memory_write` — save a memory
- `lore_context_query` — get agent-ready context

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `LORE_API_URL` | `http://localhost:3120` | Server URL |
| `LORE_API_KEY` | — | Auth key |
