package transport

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamableHTTP_ListenerKeepsRetryIntervalFromConstruction(t *testing.T) {
	orig := retryInterval
	t.Cleanup(func() { retryInterval = orig })

	var getCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getCalls.Add(1)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	retryInterval = 5 * time.Millisecond
	tr, err := NewStreamableHTTP(server.URL, WithContinuousListening())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	retryInterval = time.Hour
	require.NoError(t, tr.Start(t.Context()))
	tr.initializedOnce.Do(func() { close(tr.initialized) })

	assert.Eventually(t, func() bool { return getCalls.Load() >= 3 }, 2*time.Second, 5*time.Millisecond,
		"the listener must retry at the interval set when the transport was created")
}
