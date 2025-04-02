package server

import (
	"context"
	"github.com/redis/go-redis/v9"
	"sync"
)

var (
	_ Queue[string] = &RedisQueue[string]{}
	_ Queue[string] = &LocalQueue[string]{}
)

type Queue[T any] interface {
	New(ctx context.Context, sessionID string) error
	Publish(ctx context.Context, sessionID string, event T) error
	Subscribe(ctx context.Context, sessionID T) chan T
	PublishFromChan(ctx context.Context, sessionID string, events chan T) error
}

type RedisQueue[T any] struct {
	redisClient *redis.Client
}

func (r *RedisQueue[T]) New(ctx context.Context, sessionID string) error {
	//TODO implement me
	panic("implement me")
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

func (r *RedisQueue[T]) Publish(ctx context.Context, sessionID string, event T) error {
	_, err := r.redisClient.LPush(ctx, sessionID, event).Result()
	if err != nil {
		return err
	}
	return nil
}
func (r *RedisQueue[T]) Subscribe(ctx context.Context, sessionID string) chan T {
	return nil
}

type LocalQueue[T any] struct {
	localSessions sync.Map
}

func (l *LocalQueue[T]) New(ctx context.Context, sessionID string) error {
	//TODO implement me
	panic("implement me")
}

func (l *LocalQueue[T]) Publish(ctx context.Context, sessionID string, event T) error {
	//TODO implement me
	panic("implement me")
}

func (l *LocalQueue[T]) Subscribe(ctx context.Context, sessionID string) chan T {
	//TODO implement me
	panic("implement me")
}

func (l *LocalQueue[T]) PublishFromChan(ctx context.Context, sessionID string, events chan T) error {
	//TODO implement me
	panic("implement me")
}
