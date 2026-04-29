// Package main implements an MCP server that wraps the Lore Context REST API,
// exposing memory search, write, and context query as MCP tools.
//
// Usage:
//
//	go run .                                          # stdio transport
//	go run . -transport http -addr :8080              # HTTP transport
//	LORE_API_URL=http://localhost:3120 LORE_API_KEY=*** go run .
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// loreKey is a context key for the Lore API key.
type loreKey struct{}

// loreURLKey is a context key for the Lore API base URL.
type loreURLKey struct{}

// loreClientKey is a context key for the shared HTTP client.
type loreClientKey struct{}

// withLoreConfig adds Lore API configuration to the context.
func withLoreConfig(ctx context.Context, baseURL, apiKey string) context.Context {
	ctx = context.WithValue(ctx, loreURLKey{}, baseURL)
	ctx = context.WithValue(ctx, loreKey{}, apiKey)
	return context.WithValue(ctx, loreClientKey{}, &http.Client{Timeout: 30 * time.Second})
}

// loreConfigFromEnv injects Lore config from environment variables.
func loreConfigFromEnv(ctx context.Context) context.Context {
	return withLoreConfig(ctx,
		os.Getenv("LORE_API_URL"),
		os.Getenv("LORE_API_KEY"),
	)
}

// loreConfigFromRequest extracts the API key from HTTP request headers.
// The base URL is always taken from the server-side environment variable
// to prevent SSRF via client-supplied X-Lore-Api-Url headers.
func loreConfigFromRequest(ctx context.Context, r *http.Request) context.Context {
	return withLoreConfig(ctx,
		os.Getenv("LORE_API_URL"),
		r.Header.Get("X-Lore-Api-Key"),
	)
}

// searchRequest is the outbound payload for lore_memory_search.
type searchRequest struct {
	Query     string `json:"query"`
	TopK      int    `json:"top_k"`
	ProjectID string `json:"project_id,omitempty"`
}

// writeRequest is the outbound payload for lore_memory_write.
type writeRequest struct {
	Content    string `json:"content"`
	MemoryType string `json:"memory_type,omitempty"`
	Concepts   string `json:"concepts,omitempty"`
	ProjectID  string `json:"project_id,omitempty"`
}

// contextQueryRequest is the outbound payload for lore_context_query.
type contextQueryRequest struct {
	Query     string `json:"query"`
	ProjectID string `json:"project_id,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

// loreClient returns the HTTP client from context.
func loreClient(ctx context.Context) *http.Client {
	if c, ok := ctx.Value(loreClientKey{}).(*http.Client); ok {
		return c
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// loreRequest makes an authenticated POST request to the Lore API.
func loreRequest(ctx context.Context, path string, body any) (map[string]any, error) {
	baseURL, _ := ctx.Value(loreURLKey{}).(string)
	if baseURL == "" {
		baseURL = "http://localhost:3120"
	}

	apiKey, _ := ctx.Value(loreKey{}).(string)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := loreClient(ctx).Do(req)
	if err != nil {
		return nil, fmt.Errorf("lore request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("lore API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// loreMemorySearchTool handles the lore_memory_search MCP tool.
func loreMemorySearchTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, ok := request.GetArguments()["query"].(string)
	if !ok || query == "" {
		return mcp.NewToolResultError("missing required argument: query"), nil
	}

	topK := 10
	if v, ok := request.GetArguments()["top_k"]; ok {
		switch n := v.(type) {
		case float64:
			topK = int(n)
		case string:
			if parsed, err := strconv.Atoi(n); err == nil {
				topK = parsed
			}
		}
	}

	payload := searchRequest{
		Query: query,
		TopK:  topK,
	}
	if projectID, ok := request.GetArguments()["project_id"].(string); ok && projectID != "" {
		payload.ProjectID = projectID
	}

	result, err := loreRequest(ctx, "/v1/memory/search", payload)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(jsonBytes)), nil
}

// loreMemoryWriteTool handles the lore_memory_write MCP tool.
func loreMemoryWriteTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	content, ok := request.GetArguments()["content"].(string)
	if !ok || content == "" {
		return mcp.NewToolResultError("missing required argument: content"), nil
	}

	payload := writeRequest{
		Content: content,
	}
	if memType, ok := request.GetArguments()["memory_type"].(string); ok && memType != "" {
		payload.MemoryType = memType
	}
	if concepts, ok := request.GetArguments()["concepts"].(string); ok && concepts != "" {
		payload.Concepts = concepts
	}
	if projectID, ok := request.GetArguments()["project_id"].(string); ok && projectID != "" {
		payload.ProjectID = projectID
	}

	result, err := loreRequest(ctx, "/v1/memory/write", payload)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(jsonBytes)), nil
}

// loreContextQueryTool handles the lore_context_query MCP tool.
func loreContextQueryTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, ok := request.GetArguments()["query"].(string)
	if !ok || query == "" {
		return mcp.NewToolResultError("missing required argument: query"), nil
	}

	payload := contextQueryRequest{
		Query: query,
	}
	if projectID, ok := request.GetArguments()["project_id"].(string); ok && projectID != "" {
		payload.ProjectID = projectID
	}
	if mode, ok := request.GetArguments()["mode"].(string); ok && mode != "" {
		payload.Mode = mode
	}

	result, err := loreRequest(ctx, "/v1/context/query", payload)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(jsonBytes)), nil
}

func main() {
	transport := flag.String("transport", "stdio", "Transport: stdio or http")
	addr := flag.String("addr", ":8080", "HTTP listen address (only for http transport)")
	flag.Parse()

	mcpServer := server.NewMCPServer(
		"lore-context",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	// Register lore_memory_search tool.
	mcpServer.AddTool(
		mcp.NewTool("lore_memory_search",
			mcp.WithDescription("Search Lore Context memories by query"),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Search query text"),
			),
			mcp.WithNumber("top_k",
				mcp.Description("Maximum number of results (default 10)"),
			),
			mcp.WithString("project_id",
				mcp.Description("Optional project scope"),
			),
		),
		loreMemorySearchTool,
	)

	// Register lore_memory_write tool.
	mcpServer.AddTool(
		mcp.NewTool("lore_memory_write",
			mcp.WithDescription("Save a memory to Lore Context"),
			mcp.WithString("content",
				mcp.Required(),
				mcp.Description("Memory content to save"),
			),
			mcp.WithString("memory_type",
				mcp.Description("Memory type: fact, observation, decision, workflow, etc."),
			),
			mcp.WithString("concepts",
				mcp.Description("Comma-separated key concepts"),
			),
			mcp.WithString("project_id",
				mcp.Description("Optional project scope"),
			),
		),
		loreMemoryWriteTool,
	)

	// Register lore_context_query tool.
	mcpServer.AddTool(
		mcp.NewTool("lore_context_query",
			mcp.WithDescription("Get agent-ready context from Lore Context"),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Context query"),
			),
			mcp.WithString("project_id",
				mcp.Description("Optional project scope"),
			),
			mcp.WithString("mode",
				mcp.Description("Query mode: auto, memory, web, repo, tool_traces"),
			),
		),
		loreContextQueryTool,
	)

	switch *transport {
	case "http":
		httpServer := server.NewStreamableHTTPServer(mcpServer,
			server.WithHTTPContextFunc(loreConfigFromRequest),
		)
		log.Printf("Lore Context MCP server listening on %s", *addr)
		if err := httpServer.Start(*addr); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case "stdio":
		if err := server.ServeStdio(mcpServer,
			server.WithStdioContextFunc(loreConfigFromEnv),
		); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	default:
		log.Fatalf("unsupported transport: %s (expected \"stdio\" or \"http\")", *transport)
	}
}
