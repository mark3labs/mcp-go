package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplayCallbackCannotMutateStoredEvent(t *testing.T) {
	store := NewInMemoryEventStore()
	first, err := store.StoreEvent(t.Context(), "session", "stream", json.RawMessage(`{"first":true}`))
	require.NoError(t, err)
	_, err = store.StoreEvent(t.Context(), "session", "stream", json.RawMessage(`{"value":1}`))
	require.NoError(t, err)
	_, err = store.ReplayEventsAfter(t.Context(), "session", first, func(_ string, payload json.RawMessage) error { payload[9] = '9'; return nil })
	require.NoError(t, err)
	_, err = store.ReplayEventsAfter(t.Context(), "session", first, func(_ string, payload json.RawMessage) error {
		assert.JSONEq(t, `{"value":1}`, string(payload))
		return nil
	})
	require.NoError(t, err)
}
