package queues

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"time"
)

type RedisQueue[T any] struct {
	redisClient redis.Cmdable
	prefix      string
}

func NewRedisQueue[T any](redisClient redis.Cmdable, prefix string) *RedisQueue[T] {
	return &RedisQueue[T]{
		redisClient: redisClient,
		prefix:      prefix,
	}
}

func (r *RedisQueue[T]) New(ctx context.Context, sessionID string) error {
	return nil
}

func (r *RedisQueue[T]) PublishFromChan(ctx context.Context, sessionID string, events chan T) error {
	for event := range events {
		err := r.Publish(ctx, sessionID, event)
		if err != nil {
			return err
		}
	}
	return nil
}

// Publish serializes the event to JSON and pushes it to the Redis list.
func (r *RedisQueue[T]) Publish(ctx context.Context, sessionID string, event T) error {
	// 1. Serialize the event (e.g., to JSON)
	payload, err := json.Marshal(event)
	if err != nil {
		// Wrap error for better context
		return fmt.Errorf("failed to marshal event for redis queue: %w", err)
	}

	// 2. LPush the serialized data ([]byte or string)
	// LPush accepts interface{}, many clients handle []byte or string directly.
	cmd := r.redisClient.LPush(ctx, r.prefix+sessionID, payload)
	if err := cmd.Err(); err != nil {
		return fmt.Errorf("failed to push event to redis queue '%s': %w", sessionID, err)
	}

	slog.Debug("Published event", "sessionID", sessionID) // Optional debug log
	return nil
}

func (r *RedisQueue[T]) Subscribe(ctx context.Context, sessionID string) chan T {
	ch := make(chan T) // Channel to send deserialized events
	go func() {
		// Ensure the channel is closed when the goroutine exits.
		defer close(ch)
		defer func() {
			// Adding a log here helps confirm the goroutine actually exits
			slog.Info("Exiting subscription goroutine", "sessionID", r.prefix+sessionID)
		}()
		slog.Info("Starting subscription goroutine", "sessionID", r.prefix+sessionID)

		for {
			// BRPop blocks until an item is available, the timeout (0=indefinite) is reached,
			// or the context is canceled.
			result, err := r.redisClient.BRPop(ctx, 5*time.Second, r.prefix+sessionID).Result()
			// Log immediately after return, before checks
			slog.Debug("BRPop returned", "sessionID", sessionID, "result", result, "error", err)

			// --- Primary Check: Handle Error Returned by BRPop ---
			if err != nil {
				// Check if the error is due to context cancellation or deadline exceeded.
				// Use errors.Is for potentially wrapped errors.
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					slog.Info("Subscription context canceled or deadline exceeded (detected via BRPop error), stopping listener.",
						"sessionID", sessionID, "reason", err)
					return // Exit goroutine cleanly
				}

				// Handle redis.Nil (less likely with timeout 0, but good practice)
				// BRPop should ideally return context error, not redis.Nil on cancellation.
				if errors.Is(err, redis.Nil) {
					slog.Debug("BRPop returned redis.Nil unexpectedly (timeout 0), potentially connection issue or server behavior. Continuing loop.", "sessionID", sessionID)
					// Adding a small sleep might prevent hammering Redis if there's a persistent issue returning Nil incorrectly
					time.Sleep(100 * time.Millisecond)
					continue
				}

				// For any other errors (network issues, etc.)
				slog.Error("Error receiving from Redis queue via BRPop", "sessionID", sessionID, "error", err)
				// Depending on the error type, you might want retry logic here.
				// For simplicity, exiting on unknown errors.
				return // Exit goroutine on persistent/unknown error
			}

			// --- Secondary Check: Context state after successful BRPop (optional but safe) ---
			// If BRPop returned successfully (err == nil), but the context was cancelled *just* before
			// this check, we might want to exit anyway instead of processing/sending the potentially stale item.
			if ctx.Err() != nil {
				slog.Info("Context canceled after successful BRPop but before processing, stopping listener.", "sessionID", sessionID, "reason", ctx.Err())
				return // Exit goroutine cleanly
			}

			// --- Process Successful Result ---
			// BRPop should return [key, value] on success (err == nil)
			if len(result) != 2 {
				slog.Error("Unexpected result format from BRPop (err was nil)", "sessionID", sessionID, "result", result)
				continue // Skip malformed data
			}

			messageData := result[1] // This is a string
			var item T
			if err := json.Unmarshal([]byte(messageData), &item); err != nil {
				slog.Error("Failed to unmarshal message from Redis", "sessionID", sessionID, "rawData", messageData, "error", err)
				continue // Skip message if deserialization fails
			}

			// Send the deserialized item to the channel, checking for cancellation during send
			slog.Debug("Attempting to send item to channel", "sessionID", sessionID)
			select {
			case ch <- item:
				slog.Debug("Successfully sent item to channel", "sessionID", sessionID)
			case <-ctx.Done():
				// This catches cancellation that happens *while blocked* on sending to ch
				slog.Info("Subscription context canceled while attempting to send item to channel.", "sessionID", sessionID, "reason", ctx.Err())
				return // Exit goroutine
			}
		}
	}()

	return ch // Return the channel immediately
}
