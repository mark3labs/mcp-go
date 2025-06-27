package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *mcp.RequestId  `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type JSONRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      *mcp.RequestId `json:"id,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	logger.Info("launch successful")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request JSONRPCRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			continue
		}

		response := handleRequest(request)
		responseBytes, _ := json.Marshal(response)
		fmt.Fprintf(os.Stdout, "%s\n", responseBytes)
	}
}

func handleRequest(request JSONRPCRequest) JSONRPCResponse {
	// fmt.Fprintf(os.Stderr, "METHOD: %s PARAMS: %s\n", request.Method, string(request.Params))
	response := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
	}

	switch request.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": "1.0",
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

	case "debug/echo_error_string":
		all, _ := json.Marshal(request)
		response.Error = &struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{
			Code:    -32601,
			Message: string(all),
		}
	case "elicit/result":
		// Echo elicit/result as notification back to client
		response.Result = struct{}{}
		responseBytes, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"method":  "elicit/result_echo",
			"params":  request.Params,
		})
		fmt.Fprintf(os.Stdout, "%s\n", responseBytes)
	case "debug/send_elicit_request":
		// Parse params for ID, Prompt, Type
		var params struct {
			ID     string `json:"id"`
			Prompt string `json:"prompt"`
			Type   string `json:"type"`
		}
		_ = json.Unmarshal(request.Params, &params)
		// Send elicit/request notification to client
		notification := map[string]any{
			"jsonrpc": "2.0",
			"method":  "elicit/request",
			"params": map[string]any{
				"request": map[string]any{
					"id":     params.ID,
					"prompt": params.Prompt,
					"type":   params.Type,
				},
			},
		}
		notificationBytes, _ := json.Marshal(notification)
		fmt.Fprintf(os.Stdout, "%s\n", notificationBytes)
		response.Result = struct{}{}
	default:
		response.Error = &struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{
			Code:    -32601,
			Message: "Method not found",
		}
	}

	return response
}
