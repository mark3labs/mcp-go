package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      *int64      `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
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
	response := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
	}

	switch request.Method {
	case "initialize":
		response.Result = map[string]interface{}{
			"protocolVersion": "1.0",
			"serverInfo": map[string]interface{}{
				"name":    "mock-server",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{
				"prompts": map[string]interface{}{
					"listChanged": true,
				},
				"resources": map[string]interface{}{
					"listChanged": true,
					"subscribe":   true,
				},
				"tools": map[string]interface{}{
					"listChanged": true,
				},
			},
		}
	case "ping":
		response.Result = struct{}{}
	case "resources/list":
		response.Result = map[string]interface{}{
			"resources": []map[string]interface{}{
				{
					"name": "test-resource",
					"uri":  "test://resource",
				},
			},
		}
	case "resources/read":
		response.Result = map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"text": "test content",
					"uri":  "test://resource",
				},
			},
		}
	case "resources/subscribe", "resources/unsubscribe":
		response.Result = struct{}{}
	case "prompts/list":
		response.Result = map[string]interface{}{
			"prompts": []map[string]interface{}{
				{
					"name": "test-prompt",
				},
			},
		}
	case "prompts/get":
		response.Result = map[string]interface{}{
			"messages": []map[string]interface{}{
				{
					"role": "assistant",
					"content": map[string]interface{}{
						"type": "text",
						"text": "test message",
					},
				},
			},
		}
	case "tools/list":
		response.Result = map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name": "test-tool",
					"inputSchema": map[string]interface{}{
						"type": "object",
					},
				},
			},
		}
	case "tools/call":
		response.Result = map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "tool result",
				},
			},
		}
	case "logging/setLevel":
		response.Result = struct{}{}
	case "completion/complete":
		response.Result = map[string]interface{}{
			"completion": map[string]interface{}{
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
