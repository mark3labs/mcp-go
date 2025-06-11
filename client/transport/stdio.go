package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// Stdio implements the transport layer of the MCP protocol using stdio communication.
// It launches a subprocess and communicates with it via standard input/output streams
// using JSON-RPC messages. The client handles message routing between requests and
// responses, and supports asynchronous notifications and bidirectional requests.
type Stdio struct {
	command string
	args    []string
	env     []string

	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         *bufio.Reader
	stderr         io.ReadCloser
	responses      map[string]chan *JSONRPCResponse
	mu             sync.RWMutex
	done           chan struct{}
	onNotification func(mcp.JSONRPCNotification)
	onRequest      RequestHandler
	notifyMu       sync.RWMutex
	requestMu      sync.RWMutex
}

// NewIO returns a new stdio-based transport using existing input, output, and
// logging streams instead of spawning a subprocess.
// This is useful for testing and simulating client behavior.
func NewIO(input io.Reader, output io.WriteCloser, logging io.ReadCloser) *Stdio {
	return &Stdio{
		stdin:  output,
		stdout: bufio.NewReader(input),
		stderr: logging,

		responses: make(map[string]chan *JSONRPCResponse),
		done:      make(chan struct{}),
	}
}

// NewStdio creates a new stdio transport to communicate with a subprocess.
// It launches the specified command with given arguments and sets up stdin/stdout pipes for communication.
// Returns an error if the subprocess cannot be started or the pipes cannot be created.
func NewStdio(
	command string,
	env []string,
	args ...string,
) *Stdio {

	client := &Stdio{
		command: command,
		args:    args,
		env:     env,

		responses: make(map[string]chan *JSONRPCResponse),
		done:      make(chan struct{}),
	}

	return client
}

func (c *Stdio) Start(ctx context.Context) error {
	if err := c.spawnCommand(ctx); err != nil {
		return err
	}

	ready := make(chan struct{})
	go func() {
		close(ready)
		c.readResponses()
	}()
	<-ready

	return nil
}

// spawnCommand spawns a new process running c.command.
func (c *Stdio) spawnCommand(ctx context.Context) error {
	if c.command == "" {
		return nil
	}

	cmd := exec.CommandContext(ctx, c.command, c.args...)

	mergedEnv := os.Environ()
	mergedEnv = append(mergedEnv, c.env...)

	cmd.Env = mergedEnv

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stderr = stderr
	c.stdout = bufio.NewReader(stdout)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	return nil
}

// Close shuts down the stdio client, closing the stdin pipe and waiting for the subprocess to exit.
// Returns an error if there are issues closing stdin or waiting for the subprocess to terminate.
func (c *Stdio) Close() error {
	select {
	case <-c.done:
		return nil
	default:
	}
	// cancel all in-flight request
	close(c.done)

	if err := c.stdin.Close(); err != nil {
		return fmt.Errorf("failed to close stdin: %w", err)
	}
	if err := c.stderr.Close(); err != nil {
		return fmt.Errorf("failed to close stderr: %w", err)
	}

	if c.cmd != nil {
		return c.cmd.Wait()
	}

	return nil
}

// SetNotificationHandler sets the handler function to be called when a notification is received.
// Only one handler can be set at a time; setting a new one replaces the previous handler.
func (c *Stdio) SetNotificationHandler(
	handler func(notification mcp.JSONRPCNotification),
) {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()
	c.onNotification = handler
}

// SetRequestHandler sets the handler function to be called when a request is received from the server.
// This enables bidirectional communication for features like sampling.
func (c *Stdio) SetRequestHandler(handler RequestHandler) {
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	c.onRequest = handler
}

// readResponses continuously reads and processes messages from the server's stdout.
// It handles responses to requests, notifications, and incoming requests from the server.
// Runs until the done channel is closed or an error occurs reading from stdout.
func (c *Stdio) readResponses() {
	for {
		select {
		case <-c.done:
			return
		default:
			line, err := c.stdout.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					fmt.Printf("Error reading response: %v\n", err)
				}
				return
			}

			c.handleMessage(line)
		}
	}
}

// handleMessage processes a single JSON-RPC message, determining if it's a response,
// notification, or incoming request, and routing it appropriately.
func (c *Stdio) handleMessage(line string) {
	// Try to parse as a generic JSON-RPC message to determine type
	var baseMessage struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *mcp.RequestId  `json:"id,omitempty"`
		Method  *string         `json:"method,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   json.RawMessage `json:"error,omitempty"`
	}

	if err := json.Unmarshal([]byte(line), &baseMessage); err != nil {
		return
	}

	// Determine message type based on presence of fields
	if baseMessage.ID == nil {
		// No ID = notification
		c.handleNotification(line)
	} else if baseMessage.Method != nil {
		// Has ID and method = incoming request from server
		c.handleIncomingRequest(line)
	} else {
		// Has ID but no method = response to our request
		c.handleResponse(line)
	}
}

// handleResponse processes a response to a client request
func (c *Stdio) handleResponse(line string) {
	var response JSONRPCResponse
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		return
	}

	// Create string key for map lookup
	idKey := response.ID.String()

	c.mu.RLock()
	ch, exists := c.responses[idKey]
	c.mu.RUnlock()

	if exists {
		ch <- &response
		c.mu.Lock()
		delete(c.responses, idKey)
		c.mu.Unlock()
	}
}

// handleNotification processes a notification from the server
func (c *Stdio) handleNotification(line string) {
	var notification mcp.JSONRPCNotification
	if err := json.Unmarshal([]byte(line), &notification); err != nil {
		return
	}

	c.notifyMu.RLock()
	handler := c.onNotification
	c.notifyMu.RUnlock()

	if handler != nil {
		handler(notification)
	}
}

// handleIncomingRequest processes a request from the server and sends back a response
func (c *Stdio) handleIncomingRequest(line string) {
	var request JSONRPCRequest
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		return
	}

	// Handle the request asynchronously to avoid blocking the read loop
	go c.processIncomingRequest(request)
}

// processIncomingRequest processes an incoming request and sends the response
func (c *Stdio) processIncomingRequest(request JSONRPCRequest) {
	c.requestMu.RLock()
	handler := c.onRequest
	c.requestMu.RUnlock()

	var response struct {
		JSONRPC string        `json:"jsonrpc"`
		ID      mcp.RequestId `json:"id"`
		Result  any           `json:"result,omitempty"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    any    `json:"data,omitempty"`
		} `json:"error,omitempty"`
	}

	response.JSONRPC = mcp.JSONRPC_VERSION
	response.ID = request.ID

	if handler == nil {
		// No handler set - return METHOD_NOT_FOUND error
		response.Error = &struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    any    `json:"data,omitempty"`
		}{
			Code:    mcp.METHOD_NOT_FOUND,
			Message: "No request handler set",
		}
	} else {
		// Process the request with a timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := handler(ctx, request)
		if err != nil {
			// Convert error to JSON-RPC error
			response.Error = &struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Data    any    `json:"data,omitempty"`
			}{
				Code:    mcp.INTERNAL_ERROR,
				Message: err.Error(),
			}
		} else {
			response.Result = result
		}
	}

	// Send the response
	c.sendResponse(response)
}

// sendResponse sends a JSON-RPC response back to the server
func (c *Stdio) sendResponse(response any) {
	if c.stdin == nil {
		return
	}

	responseBytes, err := json.Marshal(response)
	if err != nil {
		return
	}
	responseBytes = append(responseBytes, '\n')

	_, _ = c.stdin.Write(responseBytes)
}

// SendRequest sends a JSON-RPC request to the server and waits for a response.
// It creates a unique request ID, sends the request over stdin, and waits for
// the corresponding response or context cancellation.
// Returns the raw JSON response message or an error if the request fails.
func (c *Stdio) SendRequest(
	ctx context.Context,
	request JSONRPCRequest,
) (*JSONRPCResponse, error) {
	if c.stdin == nil {
		return nil, fmt.Errorf("stdio client not started")
	}

	// Marshal request
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	requestBytes = append(requestBytes, '\n')

	// Create string key for map lookup
	idKey := request.ID.String()

	// Register response channel
	responseChan := make(chan *JSONRPCResponse, 1)
	c.mu.Lock()
	c.responses[idKey] = responseChan
	c.mu.Unlock()
	deleteResponseChan := func() {
		c.mu.Lock()
		delete(c.responses, idKey)
		c.mu.Unlock()
	}

	// Send request
	if _, err := c.stdin.Write(requestBytes); err != nil {
		deleteResponseChan()
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	select {
	case <-ctx.Done():
		deleteResponseChan()
		return nil, ctx.Err()
	case response := <-responseChan:
		return response, nil
	}
}

// SendNotification sends a json RPC Notification to the server.
func (c *Stdio) SendNotification(
	ctx context.Context,
	notification mcp.JSONRPCNotification,
) error {
	if c.stdin == nil {
		return fmt.Errorf("stdio client not started")
	}

	notificationBytes, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}
	notificationBytes = append(notificationBytes, '\n')

	if _, err := c.stdin.Write(notificationBytes); err != nil {
		return fmt.Errorf("failed to write notification: %w", err)
	}

	return nil
}

// Stderr returns a reader for the stderr output of the subprocess.
// This can be used to capture error messages or logs from the subprocess.
func (c *Stdio) Stderr() io.Reader {
	return c.stderr
}
