package session

import (
	"context"
	"fmt"
	"github.com/Hirocloud/mcp-go/mcp"
	"github.com/Hirocloud/mcp-go/server/queues"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"sync/atomic"
	"time"
)

const ChannelSize = 10
const NotificationQueueName = "_notification"

var _ Session = &MCMCPSession{}

// MCMCPSession (Multi-Cluster MCP Session)
type MCMCPSession struct {
	sessionID   string
	initialized atomic.Bool
	isLocal     bool
	localQueue  queues.Queue[string]
	remoteQueue queues.Queue[string]

	localQueueNotifications  queues.Queue[mcp.JSONRPCNotification]
	remoteQueueNotifications queues.Queue[mcp.JSONRPCNotification]

	localPublishQueue         chan string
	localPublishNotifications chan mcp.JSONRPCNotification

	LastUpdated time.Time
	done        chan struct{}
	cancel      context.CancelFunc
	ctx         context.Context
}

func (M *MCMCPSession) Cancel() {
	M.cancel()
}

func NewLocalMCMCPSession(ctx context.Context) (*MCMCPSession, error) {
	ctx, cancel := context.WithCancel(ctx)
	client := &MCMCPSession{
		sessionID: uuid.New().String(),
		cancel:    cancel,
		ctx:       ctx,
		isLocal:   true,
	}
	var localQueue queues.Queue[string]
	var localQueueNotifications queues.Queue[mcp.JSONRPCNotification]
	localQueue = queues.NewLocalQueue[string]()
	localQueueNotifications = queues.NewLocalQueue[mcp.JSONRPCNotification]()

	err := localQueue.New(ctx, client.sessionID)
	if err != nil {
		return nil, fmt.Errorf("session registration failed: %w", err)
	}
	localPublishQueue := make(chan string, ChannelSize)
	go func() {
		err := localQueue.PublishFromChan(ctx, client.sessionID, localPublishQueue)
		if err != nil {
			fmt.Printf("error publishing from chan: %s", err.Error())
			cancel()
		}
	}()
	client.localQueue = localQueue
	client.localPublishQueue = localPublishQueue

	err = localQueueNotifications.New(ctx, client.sessionID)
	if err != nil {
		return nil, fmt.Errorf("session registration failed: %w", err)
	}
	localPublishNotifications := make(chan mcp.JSONRPCNotification, ChannelSize)
	go func() {
		err := localQueueNotifications.PublishFromChan(ctx, client.sessionID, localPublishNotifications)
		if err != nil {
			fmt.Printf("error publishing from chan: %s", err.Error())
			cancel()
		}
	}()
	client.localQueueNotifications = localQueueNotifications
	client.localPublishNotifications = localPublishNotifications

	return client, nil
}

func NewRedisMCMCPSession(ctx context.Context, sessionID string, redis redis.Cmdable) (*MCMCPSession, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("sessionID is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	client := &MCMCPSession{
		sessionID: sessionID,
		cancel:    cancel,
		ctx:       ctx,
		isLocal:   false,
	}
	var localQueue queues.Queue[string]
	var localQueueNotifications queues.Queue[mcp.JSONRPCNotification]
	localQueue = queues.NewRedisQueue[string](redis, "")
	localQueueNotifications = queues.NewRedisQueue[mcp.JSONRPCNotification](redis, NotificationQueueName)

	err := localQueue.New(ctx, client.sessionID)
	if err != nil {
		return nil, fmt.Errorf("session registration failed: %w", err)
	}
	localPublishQueue := make(chan string, ChannelSize)
	go func() {
		err := localQueue.PublishFromChan(ctx, client.sessionID, localPublishQueue)
		if err != nil {
			fmt.Printf("error publishing from chan: %s", err.Error())
			cancel()
		}
	}()
	client.localQueue = localQueue
	client.localPublishQueue = localPublishQueue

	err = localQueueNotifications.New(ctx, client.sessionID)
	if err != nil {
		return nil, fmt.Errorf("session registration failed: %w", err)
	}
	localPublishNotifications := make(chan mcp.JSONRPCNotification, ChannelSize)
	go func() {
		err := localQueueNotifications.PublishFromChan(ctx, client.sessionID, localPublishNotifications)
		if err != nil {
			fmt.Printf("error publishing from chan: %s", err.Error())
			cancel()
		}
	}()
	client.localQueueNotifications = localQueueNotifications
	client.localPublishNotifications = localPublishNotifications
	client.initialized.Store(true)
	return client, nil
}

func (M *MCMCPSession) Context() context.Context {
	return M.ctx
}
func (M *MCMCPSession) IsLocal() bool {
	return M.isLocal
}

func (M *MCMCPSession) SessionID() string {
	return M.sessionID
}

func (M *MCMCPSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return M.localPublishNotifications
}

func (M *MCMCPSession) Initialize() {
	M.initialized.Store(true)
}

func (M *MCMCPSession) Initialized() bool {
	return M.initialized.Load()
}

func (M *MCMCPSession) QueueEvent() chan string {
	return M.localPublishQueue
}

func (M *MCMCPSession) QueueNotificationEvent() queues.Queue[mcp.JSONRPCNotification] {
	return M.localQueueNotifications
}

func (M *MCMCPSession) Queue() queues.Queue[string] {
	return M.localQueue
}
