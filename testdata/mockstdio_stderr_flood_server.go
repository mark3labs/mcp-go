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

// main writes 320KB to stderr before serving newline-delimited JSON-RPC
// requests from stdin.
func main() {
	// Write 320KB of stderr (ten 32KB drain chunks) before answering, so a
	// test can fill the drain goroutine's bounded mirror queue and exercise
	// the chunk-dropping branch while the custom stderr writer is blocked.
	filler := strings.Repeat("x", 64*1024) + "\n"
	for i := 0; i < 5; i++ {
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
