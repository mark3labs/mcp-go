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

	"github.com/mark3labs/mcp-go/mcp"
)

// Stdio implements the transport layer of the MCP protocol using stdio communication.
// It launches a subprocess and communicates with it via standard input/output streams
// using JSON-RPC messages. The client handles message routing between requests and
// responses, and supports asynchronous notifications.
type Stdio struct {
	command string
	args    []string
	env     []string

	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         *bufio.Reader
	stderr         io.ReadCloser
	responses      map[int64]chan *JSONRPCResponse
	mu             sync.RWMutex
	done           chan struct{}
	onNotification func(mcp.JSONRPCNotification)
	notifyMu       sync.RWMutex
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

		responses: make(map[int64]chan *JSONRPCResponse),
		done:      make(chan struct{}),
	}

	return client
}

func (c *Stdio) Start(ctx context.Context) error {
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

	// Start reading responses in a goroutine and wait for it to be ready
	ready := make(chan struct{})
	go func() {
		close(ready)
		c.readResponses()
	}()
	<-ready

	return nil
}

// Close shuts down the stdio client, closing the stdin pipe and waiting for the subprocess to exit.
// Returns an error if there are issues closing stdin or waiting for the subprocess to terminate.
func (c *Stdio) Close() error {
	close(c.done)
	if err := c.stdin.Close(); err != nil {
		return fmt.Errorf("failed to close stdin: %w", err)
	}
	if err := c.stderr.Close(); err != nil {
		return fmt.Errorf("failed to close stderr: %w", err)
	}
	return c.cmd.Wait()
}

// OnNotification registers a handler function to be called when notifications are received.
// Multiple handlers can be registered and will be called in the order they were added.
func (c *Stdio) SetNotificationHandler(
	handler func(notification mcp.JSONRPCNotification),
) {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()
	c.onNotification = handler
}

// readResponses continuously reads and processes responses from the server's stdout.
// It handles both responses to requests and notifications, routing them appropriately.
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

			var baseMessage JSONRPCResponse
			if err := json.Unmarshal([]byte(line), &baseMessage); err != nil {
				continue
			}

			// Handle notification
			if baseMessage.ID == nil {
				var notification mcp.JSONRPCNotification
				if err := json.Unmarshal([]byte(line), &notification); err != nil {
					continue
				}
				c.notifyMu.RLock()
				if c.onNotification != nil {
					c.onNotification(notification)
				}
				c.notifyMu.RUnlock()
				continue
			}

			c.mu.RLock()
			ch, ok := c.responses[*baseMessage.ID]
			c.mu.RUnlock()

			if ok {
				ch <- &baseMessage
				c.mu.Lock()
				delete(c.responses, *baseMessage.ID)
				c.mu.Unlock()
			}
		}
	}
}

// sendRequest sends a JSON-RPC request to the server and waits for a response.
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

	// Create the complete request structure
	responseChan := make(chan *JSONRPCResponse, 1)
	c.mu.Lock()
	c.responses[request.ID] = responseChan
	c.mu.Unlock()

	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	requestBytes = append(requestBytes, '\n')

	if _, err := c.stdin.Write(requestBytes); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.responses, request.ID)
		c.mu.Unlock()
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
