package queues

import "context"

type LocalQueue[T any] struct {
	q chan T
}

func NewLocalQueue[T any]() *LocalQueue[T] {
	return &LocalQueue[T]{
		q: make(chan T),
	}
}

func (l *LocalQueue[T]) New(ctx context.Context, sessionID string) error {
	return nil
}

func (l *LocalQueue[T]) Publish(ctx context.Context, sessionID string, event T) error {
	l.q <- event
	return nil
}

func (l *LocalQueue[T]) Subscribe(ctx context.Context, sessionID string) chan T {
	return l.q
}

func (l *LocalQueue[T]) PublishFromChan(ctx context.Context, sessionID string, events chan T) error {
	for event := range events {
		err := l.Publish(ctx, sessionID, event)
		if err != nil {
			return err
		}
	}
	return nil
}
