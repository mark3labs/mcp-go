package session

import (
	"context"
	"github.com/Hirocloud/mcp-go/mcp"
	"github.com/Hirocloud/mcp-go/server/queues"
)

type Session interface {
	SessionID() string
	NotificationChannel() chan<- mcp.JSONRPCNotification
	Initialize()
	Initialized() bool
	QueueEvent() chan string
	IsLocal() bool
	Cancel()
	Context() context.Context
	QueueNotificationEvent() queues.Queue[mcp.JSONRPCNotification]
}
