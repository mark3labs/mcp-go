package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryEventStore_ReplayCallbackMutationDoesNotCorruptStore(t *testing.T) {
	store := NewInMemoryEventStore()
	ctx := t.Context()

	first, err := store.StoreEvent(ctx, "s1", "stream", json.RawMessage(`{"n":0}`))
	require.NoError(t, err)
	_, err = store.StoreEvent(ctx, "s1", "stream", json.RawMessage(`{"n":1}`))
	require.NoError(t, err)
	_, err = store.StoreEvent(ctx, "s1", "stream", json.RawMessage(`{"n":2}`))
	require.NoError(t, err)

	var kept []json.RawMessage
	_, err = store.ReplayEventsAfter(ctx, "s1", first, func(_ string, message json.RawMessage) error {
		kept = append(kept, message)
		for i := range message {
			message[i] = 'x'
		}
		return nil
	})
	require.NoError(t, err)
	require.Len(t, kept, 2)

	var replayed []string
	streamID, err := store.ReplayEventsAfter(ctx, "s1", first, func(_ string, message json.RawMessage) error {
		replayed = append(replayed, string(message))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "stream", streamID)
	assert.Equal(t, []string{`{"n":1}`, `{"n":2}`}, replayed)
	assert.Equal(t, "xxxxxxx", string(kept[0]))
}
