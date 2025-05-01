package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	redis "github.com/redis/go-redis/v9"
)

// NewRedisEventQueueBuilder creates a builder that produces Redis-backed event queues
func NewRedisEventQueueBuilder(redisClient *redis.Client) server.SSEOption {
	return server.WithEventQueueBuilder(func(sessionID string) chan string {
		// Create a buffered channel for local use
		localChan := make(chan string, 100)

		// Key for this session's events in Redis
		redisKey := fmt.Sprintf("sse:events:%s", sessionID)

		// Start a goroutine to receive messages from Redis and forward to local channel
		go func() {
			ctx := context.Background()
			pubsub := redisClient.Subscribe(ctx, redisKey)
			defer pubsub.Close()

			ch := pubsub.Channel()
			for {
				select {
				case msg := <-ch:
					select {
					case localChan <- msg.Payload:
						// Message forwarded successfully
					default:
						// Channel is full, might need handling
					}
				case <-time.After(30 * time.Minute):
					// Timeout after inactivity
					return
				}
			}
		}()

		// Create a wrapper channel that publishes to Redis when written to
		wrappedChan := make(chan string, 100)
		go func() {
			ctx := context.Background()
			for event := range wrappedChan {
				// Forward to local channel for this instance
				select {
				case localChan <- event:
					// Also publish to Redis for other instances
					redisClient.Publish(ctx, redisKey, event)
				default:
					// Local channel is full, just publish to Redis
					redisClient.Publish(ctx, redisKey, event)
				}
			}
			close(localChan) // Close local channel when wrapped is closed
		}()

		return wrappedChan
	})
}

// NewRedisNotificationChannelBuilder creates a builder that produces Redis-backed notification channels
func NewRedisNotificationChannelBuilder(redisClient *redis.Client) server.SSEOption {
	return server.WithNotificationChannelBuilder(func(sessionID string) chan mcp.JSONRPCNotification {
		// Create a buffered channel for local use
		localChan := make(chan mcp.JSONRPCNotification, 100)

		// Key for this session's notifications in Redis
		redisKey := fmt.Sprintf("sse:notifications:%s", sessionID)

		// Start a goroutine to receive notifications from Redis and forward to local channel
		go func() {
			ctx := context.Background()
			pubsub := redisClient.Subscribe(ctx, redisKey)
			defer pubsub.Close()

			ch := pubsub.Channel()
			for {
				select {
				case msg := <-ch:
					var notification mcp.JSONRPCNotification
					if err := json.Unmarshal([]byte(msg.Payload), &notification); err == nil {
						select {
						case localChan <- notification:
							// Notification forwarded successfully
						default:
							// Channel is full, might need handling
						}
					}
				case <-time.After(30 * time.Minute):
					// Timeout after inactivity
					return
				}
			}
		}()

		// Create a wrapper channel that publishes to Redis when written to
		wrappedChan := make(chan mcp.JSONRPCNotification, 100)
		go func() {
			ctx := context.Background()
			for notification := range wrappedChan {
				notificationData, err := json.Marshal(notification)
				if err != nil {
					continue
				}

				// Forward to local channel for this instance
				select {
				case localChan <- notification:
					// Also publish to Redis for other instances
					redisClient.Publish(ctx, redisKey, string(notificationData))
				default:
					// Local channel is full, just publish to Redis
					redisClient.Publish(ctx, redisKey, string(notificationData))
				}
			}
			close(localChan) // Close local channel when wrapped is closed
		}()

		return wrappedChan
	})
}

func main() {
	// Create MCP server
	mcpServer := server.NewMCPServer(
		"example-server",
		"1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithToolCapabilities(true),
	)

	// Configure Redis client
	redisOpts := &redis.Options{
		Addr: "localhost:6379",
	}
	redisClient := redis.NewClient(redisOpts)

	// Create SSE server with Redis-backed channels
	sseServer := server.NewSSEServer(
		mcpServer,
		server.WithBaseURL("https://api.example.com"),
		server.WithBasePath("/api"),
		NewRedisEventQueueBuilder(redisClient),
		NewRedisNotificationChannelBuilder(redisClient),
	)

	// Start the server
	log.Println("Starting server on :8080")
	if err := sseServer.Start(":8080"); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
