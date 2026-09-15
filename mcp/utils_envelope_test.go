package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGetPromptResultEnvelope(t *testing.T) {
	tests := []struct {
		name          string
		raw           string
		wantNeedsIn   bool
		wantState     string
		wantRequests  int
		wantMessages  int
		wantResultTyp ResultType
	}{
		{
			name:          "input required carries round trip fields",
			raw:           `{"resultType":"input_required","requestState":"state-token","inputRequests":{"city":{"method":"elicitation/create","params":{"message":"which city?"}}},"messages":null}`,
			wantNeedsIn:   true,
			wantState:     "state-token",
			wantRequests:  1,
			wantResultTyp: ResultTypeInputRequired,
		},
		{
			name:          "complete result keeps messages",
			raw:           `{"resultType":"complete","messages":[{"role":"user","content":{"type":"text","text":"hi"}}]}`,
			wantMessages:  1,
			wantResultTyp: ResultTypeComplete,
		},
		{
			name: "absent result type is complete",
			raw:  `{"messages":[{"role":"user","content":{"type":"text","text":"hi"}}]}`,

			wantMessages: 1,
		},
		{
			name: "null messages is treated as empty",
			raw:  `{"messages":null}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(tt.raw)
			result, err := ParseGetPromptResult(&raw)
			require.NoError(t, err)

			assert.Equal(t, tt.wantResultTyp, result.ResultType)
			assert.Equal(t, tt.wantNeedsIn, result.NeedsInput())
			assert.Equal(t, tt.wantState, result.RequestState)
			assert.Len(t, result.InputRequests, tt.wantRequests)
			assert.Len(t, result.Messages, tt.wantMessages)
		})
	}
}

func TestParseReadResourceResultEnvelope(t *testing.T) {
	t.Run("input required carries round trip fields", func(t *testing.T) {
		raw := json.RawMessage(`{"resultType":"input_required","requestState":"state-token","inputRequests":{"city":{"method":"elicitation/create","params":{"message":"which city?"}}},"contents":null}`)
		result, err := ParseReadResourceResult(&raw)
		require.NoError(t, err)

		assert.True(t, result.NeedsInput())
		assert.Equal(t, "state-token", result.RequestState)
		assert.Len(t, result.InputRequests, 1)
		assert.Empty(t, result.Contents)
	})

	t.Run("cache hints survive the round trip", func(t *testing.T) {
		raw := json.RawMessage(`{"ttlMs":60000,"cacheScope":"public","contents":[{"uri":"file:///a.txt","mimeType":"text/plain","text":"hi"}]}`)
		result, err := ParseReadResourceResult(&raw)
		require.NoError(t, err)

		ttl, ok := result.TTL()
		assert.True(t, ok)
		assert.Equal(t, int64(60000), ttl)
		assert.Equal(t, CacheScopePublic, result.CacheScope)
		assert.Len(t, result.Contents, 1)
	})

	t.Run("complete result still requires contents", func(t *testing.T) {
		raw := json.RawMessage(`{"resultType":"complete"}`)
		_, err := ParseReadResourceResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "contents is missing")
	})
}

// The results the server's InputRequestBuilder produces must survive the trip
// back through the client parsers.
func TestParseResultsFromInputRequestBuilderWireForm(t *testing.T) {
	promptResult := &GetPromptResult{
		Result:               Result{ResultType: ResultTypeInputRequired},
		MultiRoundTripResult: MultiRoundTripResult{RequestState: "state-token"},
	}
	rawPrompt, err := json.Marshal(promptResult)
	require.NoError(t, err)

	promptMsg := json.RawMessage(rawPrompt)
	parsedPrompt, err := ParseGetPromptResult(&promptMsg)
	require.NoError(t, err)
	assert.True(t, parsedPrompt.NeedsInput())
	assert.Equal(t, "state-token", parsedPrompt.RequestState)

	resourceResult := &ReadResourceResult{
		MultiRoundTripResult: MultiRoundTripResult{RequestState: "state-token"},
	}
	resourceResult.ResultType = ResultTypeInputRequired
	rawResource, err := json.Marshal(resourceResult)
	require.NoError(t, err)

	resourceMsg := json.RawMessage(rawResource)
	parsedResource, err := ParseReadResourceResult(&resourceMsg)
	require.NoError(t, err)
	assert.True(t, parsedResource.NeedsInput())
	assert.Equal(t, "state-token", parsedResource.RequestState)
}
