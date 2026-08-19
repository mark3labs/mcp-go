package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *mcp.RequestId  `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type JSONRPCResponse struct {
	JSONRPC string                   `json:"jsonrpc"`
	ID      *mcp.RequestId           `json:"id,omitempty"`
	Result  any                      `json:"result,omitempty"`
	Error   *mcp.JSONRPCErrorDetails `json:"error,omitempty"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	logger.Info("launch successful")
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		line = strings.TrimRight(line, "\r\n")

		var request JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			continue
		}

		response := handleRequest(request)
		responseBytes, _ := json.Marshal(response)
		fmt.Fprintf(os.Stdout, "%s\n", responseBytes)
	}
}

func handleRequest(request JSONRPCRequest) JSONRPCResponse {
	response := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
	}

	switch request.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": mcp.LATEST_PROTOCOL_VERSION,
			"serverInfo": map[string]any{
				"name":    "mock-server",
				"version": "1.0.0",
			},
			"capabilities": map[string]any{
				"prompts": map[string]any{
					"listChanged": true,
				},
				"resources": map[string]any{
					"listChanged": true,
					"subscribe":   true,
				},
				"tools": map[string]any{
					"listChanged": true,
				},
			},
		}
	case "ping":
		response.Result = struct{}{}
	case "resources/list":
		response.Result = map[string]any{
			"resources": []map[string]any{
				{
					"name": "test-resource",
					"uri":  "test://resource",
				},
			},
		}
	case "resources/read":
		response.Result = map[string]any{
			"contents": []map[string]any{
				{
					"text": "test content",
					"uri":  "test://resource",
				},
			},
		}
	case "resources/subscribe", "resources/unsubscribe":
		response.Result = struct{}{}
	case "prompts/list":
		response.Result = map[string]any{
			"prompts": []map[string]any{
				{
					"name": "test-prompt",
				},
			},
		}
	case "prompts/get":
		response.Result = map[string]any{
			"messages": []map[string]any{
				{
					"role": "assistant",
					"content": map[string]any{
						"type": "text",
						"text": "test message",
					},
				},
			},
		}
	case "tools/list":
		response.Result = map[string]any{
			"tools": []map[string]any{
				{
					"name": "test-tool",
					"inputSchema": map[string]any{
						"type": "object",
					},
				},
			},
		}
	case "tools/call":
		response.Result = map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": "tool result",
				},
			},
		}
	case "logging/setLevel":
		response.Result = struct{}{}
	case "completion/complete":
		response.Result = map[string]any{
			"completion": map[string]any{
				"values": []string{"test completion"},
			},
		}

	// Debug methods for testing transport.
	case "debug/echo":
		response.Result = request
	case "debug/echo_notification":
		response.Result = request

		// send notification to client
		responseBytes, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"method":  "debug/test",
			"params":  request,
		})
		fmt.Fprintf(os.Stdout, "%s\n", responseBytes)

	case "debug/log_stderr":
		// Write N kilobytes to stderr, then reply. Used to reproduce the
		// stdio stderr-pipe deadlock (issue #956): without a draining reader
		// the subprocess blocks once the OS pipe buffer (~64KB) fills.
		var params struct {
			Kilobytes int `json:"kilobytes"`
		}
		_ = json.Unmarshal(request.Params, &params)
		line := strings.Repeat("x", 1023) + "\n"
		for i := 0; i < params.Kilobytes; i++ {
			_, _ = os.Stderr.WriteString(line)
		}
		response.Result = map[string]any{"logged": fmt.Sprintf("%dKB", params.Kilobytes)}
	case "debug/echo_error_string":
		all, _ := json.Marshal(request)
		details := mcp.NewJSONRPCErrorDetails(mcp.METHOD_NOT_FOUND, string(all), nil)
		response.Error = &details
	default:
		details := mcp.NewJSONRPCErrorDetails(mcp.METHOD_NOT_FOUND, "Method not found", nil)
		response.Error = &details
	}

	return response
}
