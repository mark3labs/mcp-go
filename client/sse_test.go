package client

import (
	"context"
	"github.com/stretchr/testify/assert"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type contextKey string

const (
	testHeaderKey     contextKey = "X-Test-Header"
	testHeaderFuncKey contextKey = "X-Test-Header-Func"
)

func TestSSEMCPClient(t *testing.T) {
	// Create MCP server with capabilities
	mcpServer := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithToolCapabilities(true),
		server.WithLogging(),
	)

	// Add a test tool
	mcpServer.AddTool(mcp.NewTool(
		"test-tool",
		mcp.WithDescription("Test tool"),
		mcp.WithString("parameter-1", mcp.Description("A string tool parameter")),
		mcp.WithTitleAnnotation("Test Tool Annotation Title"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	), func(ctx context.Context, requestContext server.RequestContext, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "Input parameter: " + request.GetArguments()["parameter-1"].(string),
				},
			},
		}, nil
	})
	mcpServer.AddTool(mcp.NewTool(
		"test-tool-for-http-header",
		mcp.WithDescription("Test tool for http header"),
	), func(ctx context.Context, requestContext server.RequestContext, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		//  , X-Test-Header-Func
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "context from header: " + ctx.Value(testHeaderKey).(string) + ", " + ctx.Value(testHeaderFuncKey).(string),
				},
			},
		}, nil
	})

	mcpServer.AddTool(mcp.NewTool(
		"test-tool-for-sending-notification",
		mcp.WithDescription("Test tool for sending log notification, and the log level is warn"),
	), func(ctx context.Context, requestContext server.RequestContext, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		totalProgressValue := float64(100)
		startFuncMessage := "start func"
		err := requestContext.SendProgressNotification(ctx, float64(0), &totalProgressValue, &startFuncMessage)
		if err != nil {
			return nil, err
		}

		err = requestContext.SendLoggingNotification(ctx, mcp.LoggingLevelInfo, map[string]any{
			"filtered_log_message": "will be filtered by log level",
		})
		if err != nil {
			return nil, err
		}
		err = requestContext.SendLoggingNotification(ctx, mcp.LoggingLevelError, map[string]any{
			"log_message": "log message value",
		})
		if err != nil {
			return nil, err
		}

		startFuncMessage = "end func"
		err = requestContext.SendProgressNotification(ctx, float64(100), &totalProgressValue, &startFuncMessage)
		if err != nil {
			return nil, err
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "result",
				},
			},
		}, nil
	})
	mcpServer.AddPrompt(mcp.Prompt{
		Name:        "prompt_get_for_server_notification",
		Description: "Test prompt",
	}, func(ctx context.Context, requestContext server.RequestContext, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		totalProgressValue := float64(100)
		startFuncMessage := "start get prompt"
		err := requestContext.SendProgressNotification(ctx, float64(0), &totalProgressValue, &startFuncMessage)
		if err != nil {
			return nil, err
		}

		err = requestContext.SendLoggingNotification(ctx, mcp.LoggingLevelInfo, map[string]any{
			"filtered_log_message": "will be filtered by log level",
		})
		if err != nil {
			return nil, err
		}
		err = requestContext.SendLoggingNotification(ctx, mcp.LoggingLevelError, map[string]any{
			"log_message": "log message value",
		})
		if err != nil {
			return nil, err
		}

		startFuncMessage = "end get prompt"
		err = requestContext.SendProgressNotification(ctx, float64(100), &totalProgressValue, &startFuncMessage)
		if err != nil {
			return nil, err
		}

		return &mcp.GetPromptResult{
			Messages: []mcp.PromptMessage{
				{
					Role: mcp.RoleAssistant,
					Content: mcp.TextContent{
						Type: "text",
						Text: "prompt value",
					},
				},
			},
		}, nil
	})

	mcpServer.AddResource(mcp.Resource{
		URI:  "resource://testresource",
		Name: "My Resource",
	}, func(ctx context.Context, requestContext server.RequestContext, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		totalProgressValue := float64(100)
		startFuncMessage := "start read resource"
		err := requestContext.SendProgressNotification(ctx, float64(0), &totalProgressValue, &startFuncMessage)
		if err != nil {
			return nil, err
		}

		err = requestContext.SendLoggingNotification(ctx, mcp.LoggingLevelInfo, map[string]any{
			"filtered_log_message": "will be filtered by log level",
		})
		if err != nil {
			return nil, err
		}
		err = requestContext.SendLoggingNotification(ctx, mcp.LoggingLevelError, map[string]any{
			"log_message": "log message value",
		})
		if err != nil {
			return nil, err
		}

		startFuncMessage = "end read resource"
		err = requestContext.SendProgressNotification(ctx, float64(100), &totalProgressValue, &startFuncMessage)
		if err != nil {
			return nil, err
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      "resource://testresource",
				MIMEType: "text/plain",
				Text:     "test content",
			},
		}, nil
	})

	// Initialize
	testServer := server.NewTestServer(mcpServer,
		server.WithSSEContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			ctx = context.WithValue(ctx, testHeaderKey, r.Header.Get("X-Test-Header"))
			ctx = context.WithValue(ctx, testHeaderFuncKey, r.Header.Get("X-Test-Header-Func"))
			return ctx
		}),
	)
	defer testServer.Close()

	t.Run("Can create client", func(t *testing.T) {
		client, err := NewSSEMCPClient(testServer.URL + "/sse")
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		sseTransport := client.GetTransport().(*transport.SSE)
		if sseTransport.GetBaseURL() == nil {
			t.Error("Base URL should not be nil")
		}
	})

	t.Run("Can initialize and make requests", func(t *testing.T) {
		client, err := NewSSEMCPClient(testServer.URL + "/sse")
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start the client
		if err := client.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		result, err := client.Initialize(ctx, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		if result.ServerInfo.Name != "test-server" {
			t.Errorf(
				"Expected server name 'test-server', got '%s'",
				result.ServerInfo.Name,
			)
		}

		// Test Ping
		if err := client.Ping(ctx); err != nil {
			t.Errorf("Ping failed: %v", err)
		}

		// Test ListTools
		toolsRequest := mcp.ListToolsRequest{}
		toolListResult, err := client.ListTools(ctx, toolsRequest)
		if err != nil {
			t.Errorf("ListTools failed: %v", err)
		}
		if toolListResult == nil || len((*toolListResult).Tools) == 0 {
			t.Errorf("Expected one tool")
		}
		testToolAnnotations := (*toolListResult).Tools[0].Annotations
		if testToolAnnotations.Title != "Test Tool Annotation Title" ||
			*testToolAnnotations.ReadOnlyHint != true ||
			*testToolAnnotations.DestructiveHint != false ||
			*testToolAnnotations.IdempotentHint != true ||
			*testToolAnnotations.OpenWorldHint != false {
			t.Errorf("The annotations of the tools are invalid")
		}
	})

	// t.Run("Can handle notifications", func(t *testing.T) {
	// 	client, err := NewSSEMCPClient(testServer.URL + "/sse")
	// 	if err != nil {
	// 		t.Fatalf("Failed to create client: %v", err)
	// 	}
	// 	defer client.Close()

	// 	notificationReceived := make(chan mcp.JSONRPCNotification, 1)
	// 	client.OnNotification(func(notification mcp.JSONRPCNotification) {
	// 		notificationReceived <- notification
	// 	})

	// 	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 	defer cancel()

	// 	if err := client.Start(ctx); err != nil {
	// 		t.Fatalf("Failed to start client: %v", err)
	// 	}

	// 	// Initialize first
	// 	initRequest := mcp.InitializeRequest{}
	// 	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	// 	initRequest.Params.ClientInfo = mcp.Implementation{
	// 		Name:    "test-client",
	// 		Version: "1.0.0",
	// 	}

	// 	_, err = client.Initialize(ctx, initRequest)
	// 	if err != nil {
	// 		t.Fatalf("Failed to initialize: %v", err)
	// 	}

	// 	// Subscribe to a resource to test notifications
	// 	subRequest := mcp.SubscribeRequest{}
	// 	subRequest.Params.URI = "test://resource"
	// 	if err := client.Subscribe(ctx, subRequest); err != nil {
	// 		t.Fatalf("Failed to subscribe: %v", err)
	// 	}

	// 	select {
	// 	case <-notificationReceived:
	// 		// Success
	// 	case <-time.After(time.Second):
	// 		t.Error("Timeout waiting for notification")
	// 	}
	// })

	t.Run("Handles errors properly", func(t *testing.T) {
		client, err := NewSSEMCPClient(testServer.URL + "/sse")
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Try to make a request without initializing
		toolsRequest := mcp.ListToolsRequest{}
		_, err = client.ListTools(ctx, toolsRequest)
		if err == nil {
			t.Error("Expected error when making request before initialization")
		}
	})

	// t.Run("Handles context cancellation", func(t *testing.T) {
	// 	client, err := NewSSEMCPClient(testServer.URL + "/sse")
	// 	if err != nil {
	// 		t.Fatalf("Failed to create client: %v", err)
	// 	}
	// 	defer client.Close()

	// 	if err := client.Start(context.Background()); err != nil {
	// 		t.Fatalf("Failed to start client: %v", err)
	// 	}

	// 	ctx, cancel := context.WithCancel(context.Background())
	// 	cancel() // Cancel immediately

	// 	toolsRequest := mcp.ListToolsRequest{}
	// 	_, err = client.ListTools(ctx, toolsRequest)
	// 	if err == nil {
	// 		t.Error("Expected error when context is cancelled")
	// 	}
	// })

	t.Run("CallTool", func(t *testing.T) {
		client, err := NewSSEMCPClient(testServer.URL + "/sse")
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(ctx, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.CallToolRequest{}
		request.Params.Name = "test-tool"
		request.Params.Arguments = map[string]any{
			"parameter-1": "value1",
		}

		result, err := client.CallTool(ctx, request)
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}

		if len(result.Content) != 1 {
			t.Errorf("Expected 1 content item, got %d", len(result.Content))
		}
	})

	t.Run("CallTool with customized header", func(t *testing.T) {
		client, err := NewSSEMCPClient(testServer.URL+"/sse",
			WithHeaders(map[string]string{
				"X-Test-Header": "test-header-value",
			}),
			WithHeaderFunc(func(ctx context.Context) map[string]string {
				return map[string]string{
					"X-Test-Header-Func": "test-header-func-value",
				}
			}),
		)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(ctx, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		request := mcp.CallToolRequest{}
		request.Params.Name = "test-tool-for-http-header"

		result, err := client.CallTool(ctx, request)
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}

		if len(result.Content) != 1 {
			t.Errorf("Expected 1 content item, got %d", len(result.Content))
		}
		if result.Content[0].(mcp.TextContent).Text != "context from header: test-header-value, test-header-func-value" {
			t.Errorf("Got %q, want %q", result.Content[0].(mcp.TextContent).Text, "context from header: test-header-value, test-header-func-value")
		}
	})

	t.Run("CallTool for testing log and progress notification", func(t *testing.T) {
		client, err := NewSSEMCPClient(testServer.URL + "/sse")
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		mu := sync.Mutex{}
		notificationNum := 0
		var messageNotification *mcp.JSONRPCNotification
		progressNotifications := make([]*mcp.JSONRPCNotification, 0)
		client.OnNotification(func(notification mcp.JSONRPCNotification) {
			mu.Lock()
			defer mu.Unlock()
			if notification.Method == string(mcp.MethodNotificationMessage) {
				messageNotification = &notification
			} else if notification.Method == string(mcp.MethodNotificationProgress) {
				progressNotifications = append(progressNotifications, &notification)
			}
			notificationNum += 1
		})
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(ctx, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		setLevelRequest := mcp.SetLevelRequest{}
		setLevelRequest.Params.Level = mcp.LoggingLevelWarning
		err = client.SetLevel(ctx, setLevelRequest)
		if err != nil {
			t.Errorf("SetLevel failed: %v", err)
		}

		request := mcp.CallToolRequest{}
		request.Params.Name = "test-tool-for-sending-notification"
		request.Params.Meta = &mcp.Meta{
			ProgressToken: "progress_token",
		}

		result, err := client.CallTool(ctx, request)
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}
		if len(result.Content) != 1 {
			t.Errorf("Expected 1 content item, got %d", len(result.Content))
		}

		time.Sleep(time.Millisecond * 500)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, notificationNum, 3)
		assert.NotNil(t, messageNotification)
		assert.Equal(t, messageNotification.Method, string(mcp.MethodNotificationMessage))
		assert.Equal(t, messageNotification.Params.AdditionalFields["level"], "error")
		assert.Equal(t, messageNotification.Params.AdditionalFields["data"], map[string]any{
			"log_message": "log message value",
		})

		assert.Len(t, progressNotifications, 2)
		assert.Equal(t, string(mcp.MethodNotificationProgress), progressNotifications[0].Method)
		assert.Equal(t, "start func", progressNotifications[0].Params.AdditionalFields["message"])
		assert.EqualValues(t, 0, progressNotifications[0].Params.AdditionalFields["progress"])
		assert.Equal(t, "progress_token", progressNotifications[0].Params.AdditionalFields["progressToken"])
		assert.EqualValues(t, 100, progressNotifications[0].Params.AdditionalFields["total"])

		// Assert second progress notification (end func)
		assert.Equal(t, string(mcp.MethodNotificationProgress), progressNotifications[1].Method)
		assert.Equal(t, "end func", progressNotifications[1].Params.AdditionalFields["message"])
		assert.EqualValues(t, 100, progressNotifications[1].Params.AdditionalFields["progress"])
		assert.Equal(t, "progress_token", progressNotifications[1].Params.AdditionalFields["progressToken"])
		assert.EqualValues(t, 100, progressNotifications[1].Params.AdditionalFields["total"])

	})

	t.Run("Ensure the server does not send notifications", func(t *testing.T) {
		client, err := NewSSEMCPClient(testServer.URL + "/sse")
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		mu := sync.Mutex{}
		notifications := make([]*mcp.JSONRPCNotification, 0)
		client.OnNotification(func(notification mcp.JSONRPCNotification) {
			mu.Lock()
			defer mu.Unlock()
			notifications = append(notifications, &notification)
		})
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(ctx, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		setLevelRequest := mcp.SetLevelRequest{}
		setLevelRequest.Params.Level = mcp.LoggingLevelCritical
		err = client.SetLevel(ctx, setLevelRequest)
		if err != nil {
			t.Errorf("SetLevel failed: %v", err)
		}

		// request param without progressToken
		request := mcp.CallToolRequest{}
		request.Params.Name = "test-tool-for-sending-notification"

		_, _ = client.CallTool(ctx, request)
		time.Sleep(time.Millisecond * 500)

		mu.Lock()
		defer mu.Unlock()
		assert.Len(t, notifications, 0)
	})

	t.Run("GetPrompt for testing log and progress notification", func(t *testing.T) {
		client, err := NewSSEMCPClient(testServer.URL + "/sse")
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		mu := sync.Mutex{}
		var messageNotification *mcp.JSONRPCNotification
		progressNotifications := make([]*mcp.JSONRPCNotification, 0)
		notificationNum := 0
		client.OnNotification(func(notification mcp.JSONRPCNotification) {
			mu.Lock()
			defer mu.Unlock()
			if notification.Method == string(mcp.MethodNotificationMessage) {
				messageNotification = &notification
			} else if notification.Method == string(mcp.MethodNotificationProgress) {
				progressNotifications = append(progressNotifications, &notification)
			}
			notificationNum += 1
		})
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(ctx, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		setLevelRequest := mcp.SetLevelRequest{}
		setLevelRequest.Params.Level = mcp.LoggingLevelWarning
		err = client.SetLevel(ctx, setLevelRequest)
		if err != nil {
			t.Errorf("SetLevel failed: %v", err)
		}

		request := mcp.GetPromptRequest{}
		request.Params.Name = "prompt_get_for_server_notification"
		request.Params.Meta = &mcp.Meta{
			ProgressToken: "progress_token",
		}

		result, err := client.GetPrompt(ctx, request)
		if err != nil {
			t.Fatalf("GetPrompt failed: %v", err)
		}
		assert.NotNil(t, result)
		assert.Len(t, result.Messages, 1)
		assert.Equal(t, result.Messages[0].Role, mcp.RoleAssistant)
		assert.Equal(t, result.Messages[0].Content.(mcp.TextContent).Type, "text")
		assert.Equal(t, result.Messages[0].Content.(mcp.TextContent).Text, "prompt value")
		time.Sleep(time.Millisecond * 500)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, notificationNum, 3)
		assert.NotNil(t, messageNotification)
		assert.Equal(t, messageNotification.Method, string(mcp.MethodNotificationMessage))
		assert.Equal(t, messageNotification.Params.AdditionalFields["level"], "error")
		assert.Equal(t, messageNotification.Params.AdditionalFields["data"], map[string]any{
			"log_message": "log message value",
		})

		assert.Len(t, progressNotifications, 2)
		assert.Equal(t, string(mcp.MethodNotificationProgress), progressNotifications[0].Method)
		assert.Equal(t, "start get prompt", progressNotifications[0].Params.AdditionalFields["message"])
		assert.EqualValues(t, 0, progressNotifications[0].Params.AdditionalFields["progress"])
		assert.Equal(t, "progress_token", progressNotifications[0].Params.AdditionalFields["progressToken"])
		assert.EqualValues(t, 100, progressNotifications[0].Params.AdditionalFields["total"])

		assert.Equal(t, string(mcp.MethodNotificationProgress), progressNotifications[1].Method)
		assert.Equal(t, "end get prompt", progressNotifications[1].Params.AdditionalFields["message"])
		assert.EqualValues(t, 100, progressNotifications[1].Params.AdditionalFields["progress"])
		assert.Equal(t, "progress_token", progressNotifications[1].Params.AdditionalFields["progressToken"])
		assert.EqualValues(t, 100, progressNotifications[1].Params.AdditionalFields["total"])
	})

	t.Run("GetResource for testing log and progress notification", func(t *testing.T) {
		client, err := NewSSEMCPClient(testServer.URL + "/sse")
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		mu := sync.Mutex{}
		var messageNotification *mcp.JSONRPCNotification
		progressNotifications := make([]*mcp.JSONRPCNotification, 0)
		notificationNum := 0
		client.OnNotification(func(notification mcp.JSONRPCNotification) {
			mu.Lock()
			defer mu.Unlock()
			if notification.Method == string(mcp.MethodNotificationMessage) {
				messageNotification = &notification
			} else if notification.Method == string(mcp.MethodNotificationProgress) {
				progressNotifications = append(progressNotifications, &notification)
			}
			notificationNum += 1
		})
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}

		// Initialize
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		_, err = client.Initialize(ctx, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		setLevelRequest := mcp.SetLevelRequest{}
		setLevelRequest.Params.Level = mcp.LoggingLevelWarning
		err = client.SetLevel(ctx, setLevelRequest)
		if err != nil {
			t.Errorf("SetLevel failed: %v", err)
		}

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "resource://testresource"
		request.Params.Meta = &mcp.Meta{
			ProgressToken: "progress_token",
		}

		result, err := client.ReadResource(ctx, request)
		if err != nil {
			t.Fatalf("ReadResource failed: %v", err)
		}

		assert.NotNil(t, result)
		assert.Len(t, result.Contents, 1)
		assert.Equal(t, result.Contents[0].(mcp.TextResourceContents).URI, "resource://testresource")
		assert.Equal(t, result.Contents[0].(mcp.TextResourceContents).MIMEType, "text/plain")
		assert.Equal(t, result.Contents[0].(mcp.TextResourceContents).Text, "test content")

		time.Sleep(time.Millisecond * 500)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, notificationNum, 3)
		assert.NotNil(t, messageNotification)
		assert.Equal(t, messageNotification.Method, string(mcp.MethodNotificationMessage))
		assert.Equal(t, messageNotification.Params.AdditionalFields["level"], "error")
		assert.Equal(t, messageNotification.Params.AdditionalFields["data"], map[string]any{
			"log_message": "log message value",
		})

		assert.Len(t, progressNotifications, 2)
		assert.Equal(t, string(mcp.MethodNotificationProgress), progressNotifications[0].Method)
		assert.Equal(t, "start read resource", progressNotifications[0].Params.AdditionalFields["message"])
		assert.EqualValues(t, 0, progressNotifications[0].Params.AdditionalFields["progress"])
		assert.Equal(t, "progress_token", progressNotifications[0].Params.AdditionalFields["progressToken"])
		assert.EqualValues(t, 100, progressNotifications[0].Params.AdditionalFields["total"])

		assert.Equal(t, string(mcp.MethodNotificationProgress), progressNotifications[1].Method)
		assert.Equal(t, "end read resource", progressNotifications[1].Params.AdditionalFields["message"])
		assert.EqualValues(t, 100, progressNotifications[1].Params.AdditionalFields["progress"])
		assert.Equal(t, "progress_token", progressNotifications[1].Params.AdditionalFields["progressToken"])
		assert.EqualValues(t, 100, progressNotifications[1].Params.AdditionalFields["total"])
	})
}
