package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	endpoint  = "https://search.parallel.ai/mcp"
	userAgent = "mcp-go-parallel-search-example/1.0.0"
)

func main() {
	query := flag.String("query", "", "Web search query (e.g. 'Go context cancellation examples')")
	pageURL := flag.String("url", "", "HTTP or HTTPS page to fetch, optionally after searching")
	timeout := flag.Duration("timeout", time.Minute, "Timeout for the entire example")
	flag.Parse()
	if flag.NArg() != 0 || (strings.TrimSpace(*query) == "" && strings.TrimSpace(*pageURL) == "") || *timeout <= 0 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	if err := run(ctx, endpoint, *query, *pageURL, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, serverURL, query, pageURL string, out io.Writer) error {
	c, err := client.NewStreamableHttpClient(serverURL,
		transport.WithHTTPHeaders(map[string]string{"User-Agent": userAgent}),
	)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	defer c.Close()
	if err := c.Start(ctx); err != nil {
		return fmt.Errorf("start client: %w", err)
	}

	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{Name: "mcp-go-parallel-search-example", Version: "1.0.0"}
	if _, err := c.Initialize(ctx, request); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	sessionID := uuid.NewString()
	if strings.TrimSpace(query) != "" {
		if err := callTool(ctx, c, tools.Tools, "web_search", map[string]any{
			"objective": query, "search_queries": []string{query}, "session_id": sessionID,
		}, out); err != nil {
			return err
		}
	}
	if strings.TrimSpace(pageURL) != "" {
		args := map[string]any{"urls": []string{pageURL}, "session_id": sessionID}
		if strings.TrimSpace(query) != "" {
			args["search_queries"] = []string{query}
		}
		if err := callTool(ctx, c, tools.Tools, "web_fetch", args, out); err != nil {
			return err
		}
	}
	return nil
}

func callTool(ctx context.Context, c *client.Client, tools []mcp.Tool, name string, args map[string]any, out io.Writer) error {
	found := false
	for _, tool := range tools {
		if tool.Name == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("server does not offer %s", name)
	}
	request := mcp.CallToolRequest{}
	request.Params.Name = name
	request.Params.Arguments = args
	result, err := c.CallTool(ctx, request)
	if err != nil {
		return fmt.Errorf("call %s: %w", name, err)
	}
	for _, content := range result.Content {
		if text, ok := content.(mcp.TextContent); ok {
			if _, err := fmt.Fprintln(out, text.Text); err != nil {
				return fmt.Errorf("write %s output: %w", name, err)
			}
		}
	}
	if result.IsError {
		return fmt.Errorf("%s returned a tool error", name)
	}
	return nil
}
