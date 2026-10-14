package transport

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/server"
)

func TestInProcessRegistrationHookCanCloseTransport(t *testing.T) {
	hooks := &server.Hooks{}
	s := server.NewMCPServer("test", "1", server.WithHooks(hooks))
	tr := NewInProcessTransport(s)
	var unregistered atomic.Int32
	hooks.AddOnRegisterSession(func(context.Context, server.ClientSession) { _ = tr.Close() })
	hooks.AddOnUnregisterSession(func(context.Context, server.ClientSession) { unregistered.Add(1) })
	done := make(chan error, 1)
	go func() { done <- tr.Start(t.Context()) }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrTransportClosed)
	case <-time.After(time.Second):
		t.Fatal("registration hook deadlocked closing the transport")
	}
	require.Equal(t, int32(1), unregistered.Load(), "registration must be cleaned up after the hook closes the transport")
	require.ErrorIs(t, tr.Start(t.Context()), ErrTransportClosed)
}

func TestConcurrentInProcessStartsWaitForRegistration(t *testing.T) {
	hooks := &server.Hooks{}
	s := server.NewMCPServer("test", "1", server.WithHooks(hooks))
	tr := NewInProcessTransport(s)
	entered, release := make(chan struct{}), make(chan struct{})
	var registrations atomic.Int32
	hooks.AddOnRegisterSession(func(context.Context, server.ClientSession) {
		registrations.Add(1)
		close(entered)
		<-release
	})
	results := make(chan error, 2)
	go func() { results <- tr.Start(t.Context()) }()
	<-entered
	go func() { results <- tr.Start(t.Context()) }()
	select {
	case err := <-results:
		close(release)
		t.Fatalf("Start returned before registration finished: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	require.Equal(t, int32(1), registrations.Load())
	require.NoError(t, tr.Close())
}
