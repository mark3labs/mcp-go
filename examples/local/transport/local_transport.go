package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/examples/local/server"
	"github.com/mark3labs/mcp-go/mcp"
)

type LocalTransport struct {
	server *server.LocalServer
}

func NewLocalTransport(server *server.LocalServer) *LocalTransport {
	return &LocalTransport{
		server: server,
	}
}

func (t *LocalTransport) Start(ctx context.Context) error {
	return nil
}

func (t *LocalTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	requestBytes = append(requestBytes, '\n')

	var resp mcp.JSONRPCMessage
	switch request.Method {
	case "initialize":
		resp = transport.JSONRPCResponse{}
	case "tools/list", "tools/call":
		resp = t.server.Server.HandleMessage(ctx, requestBytes)
	default:
		return nil, errors.New("not support method")
	}

	marshal, _ := json.Marshal(resp)
	rpcResp := transport.JSONRPCResponse{}
	err = json.Unmarshal(marshal, &rpcResp)
	if err != nil {
		return nil, err
	}

	return &rpcResp, nil
}

func (t *LocalTransport) SendNotification(ctx context.Context, notify mcp.JSONRPCNotification) error {
	// note ignore
	return nil
}

func (t *LocalTransport) SetNotificationHandler(handler func(notification mcp.JSONRPCNotification)) {
	// note ignore
}

func (t *LocalTransport) Close() error {
	return nil
}
