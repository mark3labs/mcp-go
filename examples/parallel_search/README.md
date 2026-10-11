# Parallel Search client

Search the web and fetch page excerpts with [Parallel Search MCP](https://docs.parallel.ai/integrations/mcp/search-mcp) using MCP-Go's Streamable HTTP client. The anonymous endpoint is free for exploration and light use, requires no account or API key, and runs searches in Fast mode. Free access has lower rate limits.

This example initializes the client, discovers tools, calls `web_search` or `web_fetch`, and prints the returned text. It makes direct tool calls without an LLM.

## Run

Install Go 1.25.5 or later. From the repository root:

```sh
go mod download
go run ./examples/parallel_search -query "Go context cancellation examples"
go run ./examples/parallel_search -url "https://go.dev/blog/context"
```

Use both flags to search and fetch a page you specify in the same session:

```sh
go run ./examples/parallel_search \
  -query "Go context cancellation examples" \
  -url "https://go.dev/blog/context"
```

The URL is supplied by you; the example does not automatically fetch search results. When both flags are set, it reuses the search query to focus the page excerpts and a single randomly generated `session_id` for both calls. No `model_name` is sent because no model is involved.

Requests go to `https://search.parallel.ai/mcp` with the example's User-Agent. The client does not load environment keys or saved credentials. Ctrl+C cancels the operation; `-timeout 30s` overrides the default one-minute timeout for the whole run. Connection errors and MCP tool errors exit with a nonzero status; tool error text is printed before exiting.
