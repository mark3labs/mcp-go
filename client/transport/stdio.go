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
	cmdFunc        CommandFunc
	stdin          io.WriteCloser
	stdout         *bufio.Reader
	stderr         io.ReadCloser
	responses      map[string]chan *JSONRPCResponse
	mu             sync.RWMutex
	done           chan struct{}
	onNotification func(mcp.JSONRPCNotification)
	notifyMu       sync.RWMutex
}

// StdioOption defines a function that configures a Stdio transport instance.
// Options can be used to customize the behavior of the transport before it starts,
// such as setting a custom command function.
type StdioOption func(*Stdio)

// CommandFunc is a factory function that returns a custom exec.Cmd used to launch the MCP subprocess.
// It can be used to apply sandboxing, custom environment control, working directories, etc.
type CommandFunc func(ctx context.Context, command string, env []string, args []string) (*exec.Cmd, error)

// WithCommandFunc sets a custom command factory function for the stdio transport.
// The CommandFunc is responsible for constructing the exec.Cmd used to launch the subprocess,
// allowing control over attributes like environment, working directory, and system-level sandboxing.
func WithCommandFunc(f CommandFunc) StdioOption {
	return func(s *Stdio) {
		s.cmdFunc = f
	}
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
	return NewStdioWithOptions(command, env, args)
}

// NewStdioWithOptions creates a new stdio transport to communicate with a subprocess.
// It launches the specified command with given arguments and sets up stdin/stdout pipes for communication.
// Returns an error if the subprocess cannot be started or the pipes cannot be created.
// Optional configuration functions can be provided to customize the transport before it starts,
// such as setting a custom command factory.
func NewStdioWithOptions(
	command string,
	env []string,
	args []string,
	opts ...StdioOption,
) *Stdio {
	s := &Stdio{
		command: command,
		args:    args,
		env:     env,

		responses: make(map[string]chan *JSONRPCResponse),
		done:      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
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

// spawnCommand spawns a new process running the configured command, args, and env.
// If an (optional) cmdFunc custom command factory function was configured, it will be used to construct the subprocess;
// otherwise, the default behavior uses exec.CommandContext with the merged environment.
// Initializes stdin, stdout, and stderr pipes for JSON-RPC communication.
func (c *Stdio) spawnCommand(ctx context.Context) error {
	if c.command == "" {
		return nil
	}

	var cmd *exec.Cmd
	var err error

	// Standard behavior if no command func present.
	if c.cmdFunc == nil {
		cmd = exec.CommandContext(ctx, c.command, c.args...)
		cmd.Env = append(os.Environ(), c.env...)
	} else if cmd, err = c.cmdFunc(ctx, c.command, c.env, c.args); err != nil {
		return err
	}

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

// GetSessionId returns the session ID of the transport.
// Since stdio does not maintain a session ID, it returns an empty string.
func (c *Stdio) GetSessionId() string {
	return ""
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
			if baseMessage.ID.IsNil() {
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

			// Create string key for map lookup
			idKey := baseMessage.ID.String()

			c.mu.RLock()
			ch, exists := c.responses[idKey]
			c.mu.RUnlock()

			if exists {
				ch <- &baseMessage
				c.mu.Lock()
				delete(c.responses, idKey)
				c.mu.Unlock()
			}
		}
	}
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
