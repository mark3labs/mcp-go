package queues

import (
	"context"
)

var (
	_ Queue[string] = &RedisQueue[string]{}
	_ Queue[string] = &LocalQueue[string]{}
)

type Queue[T any] interface {
	New(ctx context.Context, sessionID string) error
	Publish(ctx context.Context, sessionID string, event T) error
	Subscribe(ctx context.Context, sessionID string) chan T
	PublishFromChan(ctx context.Context, sessionID string, events chan T) error
}
