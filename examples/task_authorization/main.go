// This example enforces task ownership in HTTP middleware before SDK dispatch.
// Run with go run ./examples/task_authorization. The demo credentials are
// alice-token, bob-token, and operator-token, replace them with real authentication.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type principal struct {
	user    string
	canList bool
}

type principalKey struct{}
type taskTTLKey struct{}

const defaultTaskTTLMs int64 = 60000

// taskOwnership protects the application-owned task-to-user mapping.
type taskOwnership struct {
	mu     sync.Mutex
	owners map[string]string
}

func (a *taskOwnership) record(ctx context.Context, metrics server.TaskMetrics) {
	caller, ok := ctx.Value(principalKey{}).(principal)
	if !ok || caller.user == "" {
		return
	}
	a.mu.Lock()
	a.owners[metrics.TaskID] = caller.user
	a.mu.Unlock()
	ttl := defaultTaskTTLMs
	if requested, ok := ctx.Value(taskTTLKey{}).(*int64); ok && requested != nil && !server.IsModernRequest(ctx) {
		ttl = *requested
	}
	if ttl > 0 {
		time.AfterFunc(time.Duration(ttl)*time.Millisecond, func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			delete(a.owners, metrics.TaskID)
		})
	}
}

func (a *taskOwnership) allows(caller principal, taskID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	owner, known := a.owners[taskID]
	return known && owner == caller.user
}

// middleware authenticates every request and authorizes task methods from the
// JSON body. Credentials are demo tokens and must not be modified after setup.
func (a *taskOwnership) middleware(credentials map[string]principal, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		caller, known := credentials[token]
		if !bearer || !known || caller.user == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), principalKey{}, caller)
		r = r.WithContext(ctx)
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			_ = r.Body.Close()
			if err != nil {
				http.Error(w, "request body unreadable or too large", http.StatusBadRequest)
				return
			}
			var request struct {
				Method mcp.MCPMethod `json:"method"`
				Params struct {
					TaskID string          `json:"taskId"`
					Task   *mcp.TaskParams `json:"task,omitempty"`
				} `json:"params"`
			}
			if err := json.Unmarshal(body, &request); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			allowed := true
			switch request.Method {
			case mcp.MethodTasksGet, mcp.MethodTasksUpdate, mcp.MethodTasksCancel, mcp.MethodTasksResult:
				allowed = a.allows(caller, request.Params.TaskID)
			case mcp.MethodTasksList:
				allowed = caller.canList
			}
			if !allowed {
				http.Error(w, "task access denied", http.StatusForbidden)
				return
			}
			if request.Params.Task != nil {
				r = r.WithContext(context.WithValue(r.Context(), taskTTLKey{}, request.Params.Task.TTL))
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		next.ServeHTTP(w, r)
	})
}

func newTaskServer(credentials map[string]principal, options ...server.ServerOption) http.Handler {
	ownership := &taskOwnership{owners: make(map[string]string)}
	hooks := &server.TaskHooks{}
	hooks.AddOnTaskCreated(ownership.record)
	options = append([]server.ServerOption{
		server.WithTasksExtension(),
		server.WithTaskCapabilities(true, true, true),
		server.WithDefaultTaskTTL(defaultTaskTTLMs),
		server.WithMaxConcurrentTasks(16),
		server.WithTaskHooks(hooks),
	}, options...)
	srv := server.NewMCPServer("task-authorization", "1.0.0", options...)
	srv.AddTool(mcp.NewTool("approve", mcp.WithTaskSupport(mcp.TaskSupportOptional)),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if server.IsModernRequest(ctx) && len(req.Params.InputResponses) == 0 {
				return server.NewInputRequestBuilder("approval").Elicit("confirm", mcp.ElicitationParams{
					Mode:            mcp.ElicitationModeForm,
					Message:         "Approve this operation?",
					RequestedSchema: map[string]any{"type": "object"},
				}).ToolResult(), nil
			}
			return mcp.NewToolResultText("complete"), nil
		})
	srv.AddTool(mcp.NewTool("wait", mcp.WithTaskSupport(mcp.TaskSupportRequired)),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
	transport := server.NewStreamableHTTPServer(srv, server.WithStateLess(true), server.WithDisableStreaming(true))
	return ownership.middleware(credentials, transport)
}

func main() {
	address := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	credentials := map[string]principal{
		"alice-token":    {user: "alice"},
		"bob-token":      {user: "bob"},
		"operator-token": {user: "operator", canList: true},
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", newTaskServer(credentials))
	log.Printf("Task authorization example listening on %s/mcp", *address)
	log.Fatal(http.ListenAndServe(*address, mux))
}
