package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

type JSONRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      *mcp.RequestId `json:"id,omitempty"`
	Method  string         `json:"method"`
}

type JSONRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      *mcp.RequestId `json:"id,omitempty"`
	Result  any            `json:"result,omitempty"`
}

// main writes ~256KB to stderr before serving newline-delimited JSON-RPC
// requests from stdin.
func main() {
	// Write far more than an OS pipe buffer (~64KB) to stderr before answering
	// anything. A client that never drains stderr deadlocks right here: the
	// child blocks inside write(2) once the pipe fills up and stops responding
	// on stdout.
	filler := strings.Repeat("x", 64*1024) + "\n"
	for i := 0; i < 4; i++ {
		if _, err := fmt.Fprint(os.Stderr, filler); err != nil {
			return
		}
	}

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

		response := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result:  map[string]any{},
		}
		if request.Method == "ping" {
			response.Result = struct{}{}
		}

		responseBytes, _ := json.Marshal(response)
		fmt.Fprintf(os.Stdout, "%s\n", responseBytes)
	}
}
